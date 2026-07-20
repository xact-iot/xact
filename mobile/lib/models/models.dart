import 'dart:convert';

class XactUser {
  const XactUser({
    required this.id,
    required this.username,
    required this.tenantId,
    required this.roles,
    required this.allowedOrgs,
  });

  final String id;
  final String username;
  final String tenantId;
  final List<String> roles;
  final List<String> allowedOrgs;

  factory XactUser.fromJson(Map<String, dynamic> json) => XactUser(
    id: '${json['id'] ?? ''}',
    username: '${json['username'] ?? json['loginName'] ?? ''}',
    tenantId: '${json['tenant_id'] ?? ''}',
    roles: _strings(json['roles']),
    allowedOrgs: _strings(json['allowed_orgs']),
  );

  Map<String, dynamic> toJson() => {
    'id': id,
    'username': username,
    'tenant_id': tenantId,
    'roles': roles,
    'allowed_orgs': allowedOrgs,
  };
}

class AuthSession {
  const AuthSession({
    required this.serverUrl,
    required this.token,
    required this.user,
  });

  final String serverUrl;
  final String token;
  final XactUser user;

  factory AuthSession.fromJson(Map<String, dynamic> json) => AuthSession(
    serverUrl: '${json['serverUrl']}',
    token: '${json['token']}',
    user: XactUser.fromJson(json['user'] as Map<String, dynamic>),
  );

  Map<String, dynamic> toJson() => {
    'serverUrl': serverUrl,
    'token': token,
    'user': user.toJson(),
  };

  bool get isExpired {
    try {
      final parts = token.split('.');
      if (parts.length != 3) return true;
      final payload =
          jsonDecode(
                utf8.decode(base64Url.decode(base64Url.normalize(parts[1]))),
              )
              as Map<String, dynamic>;
      final expiry = (payload['exp'] as num?)?.toInt() ?? 0;
      return expiry * 1000 <= DateTime.now().millisecondsSinceEpoch ||
          '${payload['tenant_id'] ?? ''}'.isEmpty;
    } catch (_) {
      return true;
    }
  }
}

class Organisation {
  const Organisation({required this.name, required this.displayName});
  final String name;
  final String displayName;

  factory Organisation.fromJson(Map<String, dynamic> json) => Organisation(
    name: '${json['name'] ?? ''}',
    displayName: '${json['displayName'] ?? json['name'] ?? ''}',
  );
}

class TreeItem {
  TreeItem({
    required this.name,
    required this.kind,
    required this.path,
    this.description = '',
    this.value,
    this.status = '',
    this.timestamp,
    this.units = '',
    this.children = const [],
  });

  final String name;
  final String kind;
  final String path;
  final String description;
  dynamic value;
  String status;
  DateTime? timestamp;
  final String units;
  final List<TreeItem> children;

  bool get isLeaf => kind == 'leaf';

  factory TreeItem.fromJson(Map<String, dynamic> json, String parentPath) {
    final name = '${json['name'] ?? ''}';
    final path = parentPath.isEmpty ? name : '$parentPath.$name';
    final shared = json['shared'] is Map<String, dynamic>
        ? json['shared'] as Map<String, dynamic>
        : const <String, dynamic>{};
    return TreeItem(
      name: name,
      kind: '${json['type'] ?? 'node'}'.toLowerCase(),
      path: path,
      description: '${json['description'] ?? shared['description'] ?? ''}',
      value: json['value'],
      status: '${json['status'] ?? ''}',
      timestamp: _millis(json['timestamp']),
      units: '${shared['units'] ?? ''}',
      children: (json['children'] as List<dynamic>? ?? const [])
          .whereType<Map<String, dynamic>>()
          .map((child) => TreeItem.fromJson(child, path))
          .toList(),
    );
  }

  TreeItem? child(String name) {
    for (final item in children) {
      if (item.name.toLowerCase() == name.toLowerCase()) return item;
    }
    return null;
  }

  List<TreeItem> flattenLeaves() => [
    for (final item in children)
      if (item.isLeaf) item else ...item.flattenLeaves(),
  ];
}

class Device {
  Device({required this.path, required this.node, this.parentPath = ''});
  final String path;
  final TreeItem node;
  final String parentPath;

  TreeItem? get meta => node.child('meta');
  TreeItem? get kpi => node.child('kpi');

  String get name => _value(meta?.child('name')) ?? node.name;
  String get location =>
      _value(meta?.child('location')) ??
      _value(meta?.child('site')) ??
      node.description;
  String get type =>
      _value(meta?.child('type')) ??
      path.split('.').reversed.skip(1).firstOrNull ??
      'Device';

  List<TreeItem> get kpis => kpi?.flattenLeaves() ?? const [];

  static String? _value(TreeItem? item) {
    final value = item?.value;
    if (value == null || '$value'.trim().isEmpty) return null;
    return '$value';
  }
}

class MobileAppConfig {
  const MobileAppConfig({
    this.deviceParentNodes = const [],
    this.defaultDashboardName = '',
  });

  final List<String> deviceParentNodes;
  final String defaultDashboardName;

  factory MobileAppConfig.fromJson(Map<String, dynamic> json) =>
      MobileAppConfig(
        deviceParentNodes: _strings(json['deviceParentNodes']),
        defaultDashboardName: '${json['defaultDashboardName'] ?? ''}',
      );
}

class EventEntry {
  const EventEntry({
    required this.id,
    required this.timestamp,
    required this.severity,
    required this.device,
    required this.message,
    this.userName = '',
    this.notificationId = 0,
  });

  final int id;
  final DateTime timestamp;
  final String severity;
  final String device;
  final String message;
  final String userName;
  final int notificationId;

  factory EventEntry.fromJson(Map<String, dynamic> json) => EventEntry(
    id: (json['id'] as num?)?.toInt() ?? 0,
    timestamp:
        DateTime.tryParse('${json['timestamp'] ?? ''}')?.toLocal() ??
        DateTime.fromMillisecondsSinceEpoch(0),
    severity: '${json['severity'] ?? 'INFO'}'.toUpperCase(),
    device: '${json['device'] ?? ''}',
    message: '${json['message'] ?? ''}',
    userName: '${json['userName'] ?? ''}',
    notificationId: (json['notificationId'] as num?)?.toInt() ?? 0,
  );
}

class DashboardInfo {
  const DashboardInfo({
    required this.id,
    required this.name,
    this.description = '',
    this.permission = '',
  });
  final int id;
  final String name;
  final String description;
  final String permission;

  factory DashboardInfo.fromJson(Map<String, dynamic> json) => DashboardInfo(
    id: (json['id'] as num?)?.toInt() ?? 0,
    name: '${json['name'] ?? ''}',
    description: '${json['description'] ?? ''}',
    permission: '${json['permission'] ?? ''}',
  );
}

class ReportInfo {
  const ReportInfo({
    required this.id,
    required this.name,
    this.description = '',
  });
  final String id;
  final String name;
  final String description;

  factory ReportInfo.fromJson(Map<String, dynamic> json) => ReportInfo(
    id: '${json['id'] ?? ''}',
    name: '${json['name'] ?? ''}',
    description: '${json['description'] ?? ''}',
  );
}

class MobileRelease {
  const MobileRelease({
    required this.version,
    required this.downloadUrl,
    this.notes = '',
  });
  final String version;
  final String downloadUrl;
  final String notes;

  factory MobileRelease.fromJson(Map<String, dynamic> json) => MobileRelease(
    version: '${json['version'] ?? ''}',
    downloadUrl: '${json['downloadUrl'] ?? json['apkUrl'] ?? ''}',
    notes: '${json['notes'] ?? ''}',
  );
}

List<String> _strings(dynamic value) =>
    (value as List<dynamic>? ?? const []).map((item) => '$item').toList();

DateTime? _millis(dynamic value) {
  final millis = (value as num?)?.toInt();
  return millis == null || millis == 0
      ? null
      : DateTime.fromMillisecondsSinceEpoch(millis).toLocal();
}

extension _FirstOrNull<T> on Iterable<T> {
  T? get firstOrNull => isEmpty ? null : first;
}
