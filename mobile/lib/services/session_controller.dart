import 'dart:convert';

import 'package:flutter/foundation.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../models/models.dart';
import 'api_client.dart';
import 'web_session.dart';

class SessionController extends ChangeNotifier {
  SessionController({
    XactApiClient? api,
    FlutterSecureStorage? storage,
    Future<void> Function()? clearWebData,
  }) : api = api ?? XactApiClient(),
       _storage =
           storage ??
           const FlutterSecureStorage(
             iOptions: IOSOptions(
               accessibility: KeychainAccessibility.unlocked_this_device,
             ),
           ),
       _clearWebData = clearWebData ?? WebSession.clear;

  static const sessionKey = 'xact.mobile.session';
  static const _serverKey = 'xact.mobile.server';
  final XactApiClient api;
  final FlutterSecureStorage _storage;
  final Future<void> Function() _clearWebData;
  Future<void> Function()? endNotifications;
  AuthSession? _session;
  bool _initializing = true;
  bool _transitioning = false;
  int? _initialDashboardId;
  bool _notificationsEnabled = false;
  String? _lastError;

  AuthSession? get session => _session;
  bool get initializing => _initializing || _transitioning;
  bool get isAuthenticated => _session != null && !_transitioning;
  int? get initialDashboardId => _initialDashboardId;
  bool get notificationsEnabled => _notificationsEnabled;
  String? get lastError => _lastError;

  String _preferenceKey(String name) {
    final session = _session!;
    final scope = base64Url.encode(
      utf8.encode(
        jsonEncode([session.serverUrl, session.user.id, session.user.tenantId]),
      ),
    );
    return 'xact.mobile.$name.$scope';
  }

  Future<void> _loadPreferences() async {
    final prefs = await SharedPreferences.getInstance();
    _initialDashboardId = prefs.getInt(_preferenceKey('dashboard'));
    _notificationsEnabled =
        prefs.getBool(_preferenceKey('notifications')) ?? false;
  }

  Future<void> restore() async {
    // Clean legacy browser tokens and exports even if no native session remains.
    await Future.wait([_clearWebData(), api.clearDownloads()]);
    final prefs = await SharedPreferences.getInstance();
    await prefs.remove('xact.mobile.dashboard');
    await prefs.remove('xact.mobile.notifications');
    try {
      final raw = await _storage.read(key: sessionKey);
      if (raw != null) {
        final restored = AuthSession.fromJson(
          jsonDecode(raw) as Map<String, dynamic>,
        );
        if (!restored.isExpired &&
            XactApiClient.isValidServerUrl(restored.serverUrl)) {
          api.configure(serverUrl: restored.serverUrl, token: restored.token);
          _session = restored;
          await _loadPreferences();
        } else {
          await _storage.delete(key: sessionKey);
        }
      }
    } catch (_) {
      _session = null;
      api.configure(serverUrl: '');
      await _storage.delete(key: sessionKey);
    }
    _initializing = false;
    notifyListeners();
  }

  Future<String?> savedServer() async {
    final saved =
        await _storage.read(key: _serverKey) ??
        (await SharedPreferences.getInstance()).getString(_serverKey);
    return saved != null && XactApiClient.isValidServerUrl(saved)
        ? saved
        : null;
  }

  Future<void> _transition(Future<void> Function() action) async {
    if (_transitioning) {
      throw const XactApiException('An account change is already in progress.');
    }
    _transitioning = true;
    _lastError = null;
    api.invalidatePendingRequests();
    notifyListeners();
    try {
      await action();
    } catch (error) {
      _lastError = '$error';
      rethrow;
    } finally {
      _transitioning = false;
      notifyListeners();
    }
  }

  Future<void> _endSession() async {
    final server = _session?.serverUrl ?? api.serverUrl;
    _session = null;
    _notificationsEnabled = false;
    _initialDashboardId = null;
    // This is also the background push handler's authority to display messages.
    try {
      await _storage.delete(key: sessionKey);
    } finally {
      try {
        await endNotifications?.call();
      } finally {
        api.configure(serverUrl: server);
        await Future.wait([_clearWebData(), api.clearDownloads()]);
      }
    }
  }

  Future<void> _accept(AuthSession session) async {
    await _storage.write(key: sessionKey, value: jsonEncode(session.toJson()));
    await _storage.write(key: _serverKey, value: session.serverUrl);
    _session = session;
    await _loadPreferences();
  }

  Future<void> login(String server, String username, String password) =>
      _transition(() async {
        final normalized = XactApiClient.normalizeServerUrl(server);
        await _endSession();
        final signedIn = await api.login(
          serverUrl: normalized,
          username: username,
          password: password,
        );
        await _accept(signedIn);
      });

  Future<void> switchOrganisation(String org) => _transition(() async {
    final current = _session;
    if (current == null || current.user.tenantId == org) return;
    await _endSession();
    api.configure(serverUrl: current.serverUrl, token: current.token);
    try {
      await _accept(await api.switchOrganisation(current, org));
    } catch (_) {
      api.configure(serverUrl: current.serverUrl);
      rethrow;
    }
  });

  Future<void> setInitialDashboard(int? id) async {
    if (!isAuthenticated) return;
    _initialDashboardId = id;
    final key = _preferenceKey('dashboard');
    final prefs = await SharedPreferences.getInstance();
    if (id == null) {
      await prefs.remove(key);
    } else {
      await prefs.setInt(key, id);
    }
    notifyListeners();
  }

  Future<void> setNotifications(bool enabled) async {
    if (!isAuthenticated) return;
    final generation = api.generation;
    final key = _preferenceKey('notifications');
    await api.setMobileNotifications(enabled: enabled);
    api.requireCurrentSession(generation);
    _notificationsEnabled = enabled;
    await (await SharedPreferences.getInstance()).setBool(key, enabled);
    notifyListeners();
  }

  Future<void> changeServer(String server) => _transition(() async {
    final normalized = XactApiClient.normalizeServerUrl(server);
    await _endSession();
    await _storage.write(key: _serverKey, value: normalized);
    await (await SharedPreferences.getInstance()).remove(_serverKey);
    api.configure(serverUrl: normalized);
  });

  Future<void> logout() => _transition(_endSession);

  @override
  void dispose() {
    api.close();
    super.dispose();
  }
}
