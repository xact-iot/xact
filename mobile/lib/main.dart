import 'dart:async';

import 'package:flutter/material.dart';

import 'app.dart';
import 'services/notification_service.dart';
import 'services/session_controller.dart';

Future<void> main() async {
  WidgetsFlutterBinding.ensureInitialized();
  final controller = SessionController();
  final notifications = NotificationService(controller.api);
  await notifications.initialize();
  await controller.restore();
  runApp(XactMobileApp(controller: controller, notifications: notifications));
  final restored = controller.session;
  if (restored != null) {
    unawaited(_configureRestoredFirebase(notifications, restored.serverUrl));
  }
}

Future<void> _configureRestoredFirebase(
  NotificationService notifications,
  String serverUrl,
) async {
  try {
    final restartRequired = await notifications.configureForServer(serverUrl);
    if (restartRequired) await notifications.restartForFirebaseConfig();
  } catch (error) {
    // Firebase/Google Play services must never prevent access to XACT.
    debugPrint('Firebase background setup is unavailable: $error');
  }
}
