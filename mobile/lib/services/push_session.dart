import 'dart:convert';
import 'dart:math';

import 'package:flutter_secure_storage/flutter_secure_storage.dart';

import '../models/models.dart';

/// Read afresh in both foreground and background isolates; no cached preferences.
class PushSession {
  static const key = 'xact.mobile.push';
  static const sessionKey = 'xact.mobile.session';
  static const storage = FlutterSecureStorage(
    iOptions: IOSOptions(
      accessibility: KeychainAccessibility.unlocked_this_device,
    ),
  );

  static Future<Map<String, dynamic>?> read() async {
    final raw = await storage.read(key: key);
    if (raw == null) return null;
    try {
      return jsonDecode(raw) as Map<String, dynamic>;
    } catch (_) {
      return null;
    }
  }

  static Future<Map<String, dynamic>> create(AuthSession session) async {
    final random = Random.secure();
    final state = <String, dynamic>{
      'binding': base64Url.encode(
        List.generate(32, (_) => random.nextInt(256)),
      ),
      'session': session.toJson(),
      'fcmToken': '',
      'projectId': '',
    };
    await storage.write(key: key, value: jsonEncode(state));
    return state;
  }

  static Future<void> save(Map<String, dynamic> state) =>
      storage.write(key: key, value: jsonEncode(state));

  static Future<void> clear() => storage.delete(key: key);

  static Map<String, dynamic> payload(
    Map<String, dynamic> state, {
    required String device,
    required String title,
    required String body,
  }) {
    final session = AuthSession.fromJson(
      state['session'] as Map<String, dynamic>,
    );
    return {
      'binding': state['binding'],
      'userId': session.user.id,
      'orgName': session.user.tenantId,
      'device': device,
      'title': title,
      'body': body,
    };
  }

  static Future<bool> allows(Map<String, dynamic> data) async {
    try {
      final state = await read();
      if (state == null ||
          data['binding'] is! String ||
          (data['binding'] as String).isEmpty ||
          data['binding'] != state['binding']) {
        return false;
      }
      final raw = await storage.read(key: sessionKey);
      if (raw == null) return false;
      final current = AuthSession.fromJson(
        jsonDecode(raw) as Map<String, dynamic>,
      );
      final registered = AuthSession.fromJson(
        state['session'] as Map<String, dynamic>,
      );
      return !current.isExpired &&
          current.token == registered.token &&
          current.serverUrl == registered.serverUrl &&
          current.user.id == registered.user.id &&
          current.user.tenantId == registered.user.tenantId &&
          data['userId'] == current.user.id &&
          data['orgName'] == current.user.tenantId;
    } catch (_) {
      return false;
    }
  }
}
