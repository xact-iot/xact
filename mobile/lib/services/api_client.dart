import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:http/http.dart' as http;
import 'package:path_provider/path_provider.dart';

import '../models/models.dart';

class XactApiException implements Exception {
  const XactApiException(this.message, {this.statusCode});
  final String message;
  final int? statusCode;

  @override
  String toString() => message;
}

class FirebaseClientConfig {
  const FirebaseClientConfig({
    required this.configured,
    this.projectId = '',
    this.appId = '',
    this.apiKey = '',
    this.messagingSenderId = '',
  });

  factory FirebaseClientConfig.fromJson(Map<String, dynamic> json) =>
      FirebaseClientConfig(
        configured: json['configured'] == true,
        projectId: '${json['projectId'] ?? ''}',
        appId: '${json['appId'] ?? ''}',
        apiKey: '${json['apiKey'] ?? ''}',
        messagingSenderId: '${json['messagingSenderId'] ?? ''}',
      );

  final bool configured;
  final String projectId;
  final String appId;
  final String apiKey;
  final String messagingSenderId;

  bool get isComplete =>
      configured &&
      projectId.isNotEmpty &&
      appId.isNotEmpty &&
      apiKey.isNotEmpty &&
      messagingSenderId.isNotEmpty;

  Map<String, String> toJson() => {
    'projectId': projectId,
    'appId': appId,
    'apiKey': apiKey,
    'messagingSenderId': messagingSenderId,
  };
}

class XactApiClient {
  XactApiClient({http.Client? client}) : _client = client ?? http.Client();

  final http.Client _client;
  String _serverUrl = '';
  String? _token;
  String _tenantId = '';

  String get serverUrl => _serverUrl;
  String? get token => _token;

  void configure({required String serverUrl, String? token}) {
    _serverUrl = token == null
        ? normalizeServerUrl(serverUrl)
        : serverUrl.trim().replaceAll(RegExp(r'/+$'), '');
    _token = token;
    _tenantId = _tenantFromToken(token);
  }

  static String normalizeServerUrl(String input) {
    var value = input.trim();
    if (value.isEmpty) return '';
    if (!value.contains('://')) value = 'https://$value';
    var uri = Uri.parse(value);
    var path = uri.path.replaceAll(RegExp(r'/+$'), '');
    if (path.isEmpty) path = '/xact';
    uri = uri.replace(path: path, query: null, fragment: null);
    return uri.toString().replaceAll(RegExp(r'/+$'), '');
  }

  Uri uri(String path, [Map<String, dynamic>? query]) {
    final clean = path.startsWith('/') ? path : '/$path';
    final base = Uri.parse('$_serverUrl$clean');
    if (query == null) return base;
    return base.replace(
      queryParameters: {
        for (final entry in query.entries)
          if (entry.value != null && '${entry.value}'.isNotEmpty)
            entry.key: '${entry.value}',
      },
    );
  }

  Map<String, String> get _headers => {
    'Accept': 'application/json',
    'Content-Type': 'application/json',
    if (_token != null) 'Authorization': 'Bearer $_token',
  };

  Future<Map<String, dynamic>> health() => _jsonMap('GET', '/health');

  Future<AuthSession> login({
    required String serverUrl,
    required String username,
    required String password,
  }) async {
    final normalized = normalizeServerUrl(serverUrl);
    final parsed = Uri.parse(normalized);
    final candidates = <String>[
      normalized,
      if (parsed.path == '/xact')
        parsed.replace(path: '', query: null, fragment: null).toString(),
    ];
    Map<String, dynamic>? body;
    XactApiException? lastError;
    for (final candidate in candidates) {
      _serverUrl = candidate.replaceAll(RegExp(r'/+$'), '');
      _token = null;
      _tenantId = '';
      try {
        body = await _jsonMap(
          'POST',
          '/login',
          body: {'username': username.trim(), 'password': password},
          authenticated: false,
        );
        break;
      } on XactApiException catch (error) {
        lastError = error;
        if (error.statusCode != 404) rethrow;
      }
    }
    if (body == null) {
      throw lastError ?? const XactApiException('Login failed.');
    }
    final session = AuthSession(
      serverUrl: _serverUrl,
      token: '${body['token'] ?? ''}',
      user: XactUser.fromJson(body['user'] as Map<String, dynamic>),
    );
    if (session.token.isEmpty || session.user.tenantId.isEmpty) {
      throw const XactApiException('The server returned an invalid session.');
    }
    configure(serverUrl: _serverUrl, token: session.token);
    return session;
  }

  Future<FirebaseClientConfig> firebaseConfigForServer(String serverUrl) async {
    final normalized = normalizeServerUrl(serverUrl);
    final parsed = Uri.parse(normalized);
    final candidates = <String>[
      normalized,
      if (parsed.path == '/xact')
        parsed.replace(path: '', query: null, fragment: null).toString(),
    ];
    XactApiException? lastError;
    for (final candidate in candidates) {
      final base = candidate.replaceAll(RegExp(r'/+$'), '');
      try {
        final response = await _client
            .get(
              Uri.parse('$base/api/v1/mobile/firebase-config'),
              headers: const {'Accept': 'application/json'},
            )
            .timeout(const Duration(seconds: 12));
        if (response.statusCode == 404) continue;
        _ensureSuccess(response);
        dynamic decoded;
        try {
          decoded = jsonDecode(response.body);
        } catch (_) {
          lastError = const XactApiException(
            'The server returned an invalid Firebase configuration.',
          );
          continue;
        }
        if (decoded is! Map<String, dynamic>) {
          lastError = const XactApiException(
            'The server returned an invalid Firebase configuration.',
          );
          continue;
        }
        return FirebaseClientConfig.fromJson(decoded);
      } on XactApiException catch (error) {
        lastError = error;
        if (error.statusCode != 404) rethrow;
      } on SocketException catch (error) {
        throw XactApiException('Cannot reach the XACT server: $error');
      }
    }
    if (lastError != null && lastError.statusCode != 404) throw lastError;
    return const FirebaseClientConfig(configured: false);
  }

  Future<AuthSession> switchOrganisation(
    AuthSession current,
    String org,
  ) async {
    final body = await _jsonMap(
      'POST',
      '/api/v1/auth/switch-org',
      body: {'org': org},
    );
    final session = AuthSession(
      serverUrl: current.serverUrl,
      token: '${body['token'] ?? ''}',
      user: XactUser.fromJson(body['user'] as Map<String, dynamic>),
    );
    configure(serverUrl: current.serverUrl, token: session.token);
    return session;
  }

  Future<List<Organisation>> organisations() async {
    try {
      final body = await _jsonMap('GET', '/api/v1/auth/my-orgs');
      return (body['orgs'] as List<dynamic>? ?? const [])
          .whereType<Map<String, dynamic>>()
          .map(Organisation.fromJson)
          .toList();
    } on XactApiException catch (error) {
      if (error.statusCode != 404 && error.statusCode != 403) rethrow;
      return const [];
    }
  }

  Future<MobileAppConfig> mobileAppConfig() async {
    try {
      return MobileAppConfig.fromJson(
        await _jsonMap('GET', '/api/v1/mobile/config'),
      );
    } on XactApiException catch (error) {
      if (error.statusCode == 404 || error.statusCode == 403) {
        return const MobileAppConfig();
      }
      rethrow;
    }
  }

  Future<List<Device>> devices({List<String>? parentNodes}) async {
    final configuredParents =
        parentNodes ?? (await mobileAppConfig()).deviceParentNodes;
    if (configuredParents.isEmpty) {
      return _devicesBelow('');
    }
    final groups = await Future.wait(
      configuredParents.map((parent) => _devicesBelow(parent)),
    );
    final found = <Device>[];
    final paths = <String>{};
    for (final group in groups) {
      for (final device in group) {
        if (paths.add(device.path)) found.add(device);
      }
    }
    found.sort((a, b) => a.name.toLowerCase().compareTo(b.name.toLowerCase()));
    return found;
  }

  Future<List<Device>> _devicesBelow(String parentPath) async {
    final encoded = parentPath
        .split('.')
        .where((part) => part.isNotEmpty)
        .map(Uri.encodeComponent)
        .join('/');
    final body = await _jsonMap(
      'GET',
      encoded.isEmpty ? '/api/v1/nodes/' : '/api/v1/nodes/$encoded',
      query: {'depth': -1},
    );
    final rootPath = '${body['path'] ?? ''}'
        .replaceAll('/', '.')
        .replaceAll(RegExp(r'^\.+|\.+$'), '');
    final root = TreeItem(
      name: '${body['name'] ?? ''}',
      kind: 'node',
      path: rootPath,
      description: '${body['description'] ?? ''}',
      children: (body['children'] as List<dynamic>? ?? const [])
          .whereType<Map<String, dynamic>>()
          .map((child) => TreeItem.fromJson(child, rootPath))
          .toList(),
    );
    final found = <Device>[];
    void walk(TreeItem node) {
      if (node.child('kpi') != null && node.child('meta') != null) {
        found.add(Device(path: node.path, node: node, parentPath: parentPath));
        return;
      }
      for (final child in node.children.where((item) => !item.isLeaf)) {
        walk(child);
      }
    }

    walk(root);
    found.sort((a, b) => a.name.toLowerCase().compareTo(b.name.toLowerCase()));
    return found;
  }

  Future<Device> device(String relativePath) async {
    var requestPath = relativePath;
    if (_tenantId.isNotEmpty && requestPath.startsWith('$_tenantId.')) {
      requestPath = requestPath.substring(_tenantId.length + 1);
    }
    final encoded = requestPath
        .split('.')
        .where((part) => part.isNotEmpty)
        .map(Uri.encodeComponent)
        .join('/');
    final body = await _jsonMap(
      'GET',
      '/api/v1/nodes/$encoded',
      query: {'depth': -1},
    );
    final responsePath = '${body['path'] ?? relativePath}'
        .replaceAll('/', '.')
        .replaceAll(RegExp(r'^\.+|\.+$'), '');
    final root = TreeItem(
      name: '${body['name'] ?? relativePath.split('.').last}',
      kind: 'node',
      path: responsePath,
      description: '${body['description'] ?? ''}',
      children: (body['children'] as List<dynamic>? ?? const [])
          .whereType<Map<String, dynamic>>()
          .map((child) => TreeItem.fromJson(child, responsePath))
          .toList(),
    );
    return Device(path: responsePath, node: root);
  }

  Future<List<EventEntry>> events({
    int limit = 30,
    String? device,
    int? afterId,
  }) async {
    final body = await _jsonList(
      'GET',
      '/api/v1/logs',
      query: {'limit': limit, 'device': device, 'after_id': afterId},
    );
    return body
        .whereType<Map<String, dynamic>>()
        .map(EventEntry.fromJson)
        .toList();
  }

  Future<List<DashboardInfo>> dashboards() async {
    final responses = await Future.wait<dynamic>([
      _jsonList('GET', '/api/v1/dashboards'),
      _jsonMap('GET', '/api/v1/permissions'),
    ]);
    final body = responses[0] as List<dynamic>;
    final permissions = responses[1] as Map<String, dynamic>;
    return body
        .whereType<Map<String, dynamic>>()
        .where((item) => item['isCategory'] != true)
        .map(DashboardInfo.fromJson)
        .where(
          (dashboard) =>
              dashboard.permission.isEmpty ||
              ((permissions[dashboard.permission] as Map?)?['view'] == true),
        )
        .toList();
  }

  Future<List<ReportInfo>> reports() async {
    final body = await _jsonList('GET', '/api/v1/reports/templates');
    return body
        .whereType<Map<String, dynamic>>()
        .map(ReportInfo.fromJson)
        .toList();
  }

  Future<File> downloadReport(ReportInfo report) async {
    final response = await _request(
      'POST',
      '/api/v1/reports/generate',
      body: {'templateId': report.id, 'variables': <String, String>{}},
    );
    final directory = await getTemporaryDirectory();
    final safeName = report.name.replaceAll(RegExp(r'[^A-Za-z0-9._-]+'), '_');
    final file = File(
      '${directory.path}/${safeName.isEmpty ? 'report' : safeName}.pdf',
    );
    await file.writeAsBytes(response.bodyBytes, flush: true);
    return file;
  }

  Future<Map<String, dynamic>> natsConfig() =>
      _jsonMap('GET', '/api/v1/system/nats-config');

  Future<Map<String, dynamic>> myProfile() => _jsonMap('GET', '/api/v1/me/');

  Future<void> setMobileNotifications({required bool enabled}) async {
    final profile = await myProfile();
    final options = profile['notificationOptions'] is Map<String, dynamic>
        ? Map<String, dynamic>.from(profile['notificationOptions'] as Map)
        : <String, dynamic>{};
    options['mobileEnabled'] = enabled;
    if (!enabled) options['fcmEnabled'] = false;
    await _jsonMap(
      'PUT',
      '/api/v1/me/',
      body: {'notificationOptions': options},
    );
  }

  Future<void> setFcmRegistrationToken(String token, String projectId) async {
    final profile = await myProfile();
    final options = profile['notificationOptions'] is Map<String, dynamic>
        ? Map<String, dynamic>.from(profile['notificationOptions'] as Map)
        : <String, dynamic>{};
    options['fcmEnabled'] = token.isNotEmpty;
    options['fcmToken'] = token;
    options['fcmProjectId'] = projectId;
    await _jsonMap(
      'PUT',
      '/api/v1/me/',
      body: {'notificationOptions': options},
    );
  }

  Future<MobileRelease?> mobileRelease() async {
    try {
      final body = await _jsonMap('GET', '/api/v1/mobile/release');
      final release = MobileRelease.fromJson(body);
      return release.version.isEmpty || release.downloadUrl.isEmpty
          ? null
          : release;
    } on XactApiException catch (error) {
      if (error.statusCode == 404 || error.statusCode == 403) return null;
      rethrow;
    }
  }

  Future<File> downloadApk(MobileRelease release) async {
    final releaseUri = Uri.parse(release.downloadUrl);
    final target = releaseUri.hasScheme ? releaseUri : uri(release.downloadUrl);
    final response = await _client.get(target, headers: _headers);
    _ensureSuccess(response);
    final directory = await getTemporaryDirectory();
    final file = File('${directory.path}/xact-${release.version}.apk');
    await file.writeAsBytes(response.bodyBytes, flush: true);
    return file;
  }

  String dashboardUrl(int? dashboardId) {
    final suffix = dashboardId == null
        ? ''
        : '#${Uri.encodeComponent('$dashboardId')}';
    return '$_serverUrl/?embedded=dashboard$suffix';
  }

  Future<Map<String, dynamic>> _jsonMap(
    String method,
    String path, {
    Map<String, dynamic>? body,
    Map<String, dynamic>? query,
    bool authenticated = true,
  }) async {
    final response = await _request(
      method,
      path,
      body: body,
      query: query,
      authenticated: authenticated,
    );
    if (response.body.trim().isEmpty) return <String, dynamic>{};
    final decoded = jsonDecode(response.body);
    if (decoded is! Map<String, dynamic>) {
      throw const XactApiException(
        'The server returned an unexpected response.',
      );
    }
    return decoded;
  }

  Future<List<dynamic>> _jsonList(
    String method,
    String path, {
    Map<String, dynamic>? query,
  }) async {
    final response = await _request(method, path, query: query);
    final decoded = jsonDecode(response.body);
    if (decoded is! List<dynamic>) {
      throw const XactApiException(
        'The server returned an unexpected response.',
      );
    }
    return decoded;
  }

  Future<http.Response> _request(
    String method,
    String path, {
    Map<String, dynamic>? body,
    Map<String, dynamic>? query,
    bool authenticated = true,
  }) async {
    if (_serverUrl.isEmpty) {
      throw const XactApiException('Enter the XACT server URL.');
    }
    final headers = Map<String, String>.from(_headers);
    if (!authenticated) headers.remove('Authorization');
    final target = uri(path, query);
    final attempts = method == 'GET' ? 2 : 1;
    for (var attempt = 0; attempt < attempts; attempt++) {
      try {
        final Future<http.Response> request = switch (method) {
          'POST' => _client.post(
            target,
            headers: headers,
            body: jsonEncode(body ?? {}),
          ),
          'PUT' => _client.put(
            target,
            headers: headers,
            body: jsonEncode(body ?? {}),
          ),
          _ => _client.get(target, headers: headers),
        };
        final response = await request.timeout(const Duration(seconds: 25));
        _ensureSuccess(response);
        return response;
      } on XactApiException {
        rethrow;
      } on SocketException {
        if (attempt + 1 < attempts) {
          await Future<void>.delayed(const Duration(milliseconds: 250));
          continue;
        }
        throw const XactApiException('Could not reach the XACT server.');
      } on TimeoutException {
        if (attempt + 1 < attempts) continue;
        throw const XactApiException('The XACT server did not respond.');
      } on http.ClientException {
        if (attempt + 1 < attempts) {
          await Future<void>.delayed(const Duration(milliseconds: 250));
          continue;
        }
        throw const XactApiException('Could not reach the XACT server.');
      } catch (error) {
        throw XactApiException('Connection failed: $error');
      }
    }
    throw const XactApiException('Could not reach the XACT server.');
  }

  void _ensureSuccess(http.Response response) {
    if (response.statusCode >= 200 && response.statusCode < 300) return;
    var message = 'Request failed (${response.statusCode}).';
    try {
      final body = jsonDecode(response.body) as Map<String, dynamic>;
      message = '${body['error'] ?? body['message'] ?? message}';
    } catch (_) {}
    throw XactApiException(message, statusCode: response.statusCode);
  }

  void close() => _client.close();

  String _tenantFromToken(String? token) {
    if (token == null) return '';
    try {
      final part = token.split('.')[1];
      final payload =
          jsonDecode(utf8.decode(base64Url.decode(base64Url.normalize(part))))
              as Map<String, dynamic>;
      return '${payload['tenant_id'] ?? ''}';
    } catch (_) {
      return '';
    }
  }
}
