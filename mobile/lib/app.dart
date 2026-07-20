import 'package:flutter/material.dart';

import 'screens/home_shell.dart';
import 'screens/login_screen.dart';
import 'services/notification_service.dart';
import 'services/session_controller.dart';
import 'theme.dart';
import 'widgets/common.dart';

class XactMobileApp extends StatelessWidget {
  const XactMobileApp({
    super.key,
    required this.controller,
    required this.notifications,
  });

  final SessionController controller;
  final NotificationService notifications;

  @override
  Widget build(BuildContext context) => MaterialApp(
    title: 'XACT Mobile',
    debugShowCheckedModeBanner: false,
    theme: buildXactTheme(),
    home: AnimatedBuilder(
      animation: controller,
      builder: (context, _) {
        if (controller.initializing) {
          return const Scaffold(body: LoadingView(label: 'Starting XACT'));
        }
        final session = controller.session;
        if (session == null) return LoginScreen(controller: controller);
        return HomeShell(
          key: ValueKey('${session.user.tenantId}:${session.token}'),
          controller: controller,
          notifications: notifications,
        );
      },
    ),
  );
}
