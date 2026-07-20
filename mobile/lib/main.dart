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
}
