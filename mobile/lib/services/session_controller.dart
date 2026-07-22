import 'dart:convert';

import 'package:flutter/foundation.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../models/models.dart';
import 'api_client.dart';

class SessionController extends ChangeNotifier {
  SessionController({XactApiClient? api, FlutterSecureStorage? storage})
    : api = api ?? XactApiClient(),
      _storage = storage ?? const FlutterSecureStorage();

  static const _sessionKey = 'xact.mobile.session';
  static const _serverKey = 'xact.mobile.server';
  static const _dashboardKey = 'xact.mobile.dashboard';
  static const _notificationsKey = 'xact.mobile.notifications';

  final XactApiClient api;
  final FlutterSecureStorage _storage;
  AuthSession? _session;
  bool _initializing = true;
  int? _initialDashboardId;
  bool _notificationsEnabled = false;

  AuthSession? get session => _session;
  bool get initializing => _initializing;
  bool get isAuthenticated => _session != null;
  int? get initialDashboardId => _initialDashboardId;
  bool get notificationsEnabled => _notificationsEnabled;

  Future<void> restore() async {
    final prefs = await SharedPreferences.getInstance();
    _initialDashboardId = prefs.getInt(_dashboardKey);
    _notificationsEnabled = prefs.getBool(_notificationsKey) ?? false;
    try {
      final raw = await _storage.read(key: _sessionKey);
      if (raw != null) {
        final restored = AuthSession.fromJson(
          jsonDecode(raw) as Map<String, dynamic>,
        );
        if (!restored.isExpired) {
          _session = restored;
          api.configure(serverUrl: restored.serverUrl, token: restored.token);
        } else {
          await _storage.delete(key: _sessionKey);
        }
      }
    } catch (_) {
      await _storage.delete(key: _sessionKey);
    }
    _initializing = false;
    notifyListeners();
  }

  Future<String?> savedServer() async {
    final secure = await _storage.read(key: _serverKey);
    return secure ??
        (await SharedPreferences.getInstance()).getString(_serverKey);
  }

  Future<void> login(String server, String username, String password) async {
    final signedIn = await api.login(
      serverUrl: server,
      username: username,
      password: password,
    );
    _session = signedIn;
    await _persist(signedIn);
    notifyListeners();
  }

  Future<void> switchOrganisation(String org) async {
    final current = _session;
    if (current == null || current.user.tenantId == org) return;
    final switched = await api.switchOrganisation(current, org);
    _session = switched;
    await _persist(switched);
    notifyListeners();
  }

  Future<void> setInitialDashboard(int? id) async {
    _initialDashboardId = id;
    final prefs = await SharedPreferences.getInstance();
    if (id == null) {
      await prefs.remove(_dashboardKey);
    } else {
      await prefs.setInt(_dashboardKey, id);
    }
    notifyListeners();
  }

  Future<void> setNotifications(bool enabled) async {
    await api.setMobileNotifications(enabled: enabled);
    _notificationsEnabled = enabled;
    await (await SharedPreferences.getInstance()).setBool(
      _notificationsKey,
      enabled,
    );
    notifyListeners();
  }

  Future<void> changeServer(String server) async {
    final normalized = XactApiClient.normalizeServerUrl(server);
    await _storage.write(key: _serverKey, value: normalized);
    await (await SharedPreferences.getInstance()).setString(
      _serverKey,
      normalized,
    );
    _session = null;
    api.configure(serverUrl: normalized);
    await _storage.delete(key: _sessionKey);
    notifyListeners();
  }

  Future<void> logout() async {
    final server = _session?.serverUrl ?? api.serverUrl;
    _session = null;
    api.configure(serverUrl: server);
    await _storage.delete(key: _sessionKey);
    notifyListeners();
  }

  Future<void> _persist(AuthSession value) async {
    await _storage.write(key: _sessionKey, value: jsonEncode(value.toJson()));
    await _storage.write(key: _serverKey, value: value.serverUrl);
    await (await SharedPreferences.getInstance()).setString(
      _serverKey,
      value.serverUrl,
    );
  }

  @override
  void dispose() {
    api.close();
    super.dispose();
  }
}
