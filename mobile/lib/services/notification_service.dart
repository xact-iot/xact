import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter_local_notifications/flutter_local_notifications.dart';

import '../models/models.dart';
import 'api_client.dart';
import 'realtime_service.dart';

class NotificationService {
  NotificationService(this.api);
  final XactApiClient api;
  final FlutterLocalNotificationsPlugin _plugin =
      FlutterLocalNotificationsPlugin();
  Timer? _timer;
  StreamSubscription<MobileNotification>? _realtimeSubscription;
  RealtimeService? _realtime;
  bool _enabled = false;
  int _lastEventId = 0;
  ValueChanged<String>? onDeviceSelected;

  Future<void> initialize() async {
    if (kIsWeb) return;
    await _plugin.initialize(
      settings: const InitializationSettings(
        android: AndroidInitializationSettings('@mipmap/ic_launcher'),
        iOS: DarwinInitializationSettings(
          requestAlertPermission: false,
          requestBadgePermission: false,
          requestSoundPermission: false,
        ),
      ),
      onDidReceiveNotificationResponse: (response) {
        final device = response.payload;
        if (device != null && device.isNotEmpty) onDeviceSelected?.call(device);
      },
    );
  }

  Future<void> start() async {
    stop();
    _enabled = true;
    try {
      final latest = await api.events(limit: 1);
      if (latest.isNotEmpty) _lastEventId = latest.first.id;
    } catch (_) {}
    _timer = Timer.periodic(const Duration(seconds: 30), (_) => _poll());
  }

  void bindRealtime(RealtimeService realtime) {
    _realtime = realtime;
    _realtimeSubscription?.cancel();
    _realtimeSubscription = realtime.notifications.listen((notification) {
      if (!_enabled) return;
      _plugin.show(
        id: DateTime.now().millisecondsSinceEpoch.remainder(2147483647),
        title: notification.title,
        body: notification.body,
        payload: notification.device,
        notificationDetails: _details,
      );
    });
  }

  void stop() {
    _enabled = false;
    _timer?.cancel();
    _timer = null;
  }

  Future<void> dispose() async {
    stop();
    await _realtimeSubscription?.cancel();
  }

  Future<bool> requestPermission() async {
    if (kIsWeb) return true;
    final android = _plugin
        .resolvePlatformSpecificImplementation<
          AndroidFlutterLocalNotificationsPlugin
        >();
    final androidAllowed = await android?.requestNotificationsPermission();
    final ios = _plugin
        .resolvePlatformSpecificImplementation<
          IOSFlutterLocalNotificationsPlugin
        >();
    final iosAllowed = await ios?.requestPermissions(
      alert: true,
      badge: true,
      sound: true,
    );
    return androidAllowed ?? iosAllowed ?? true;
  }

  Future<void> _poll() async {
    if (!_enabled || (_realtime?.connected ?? false)) return;
    try {
      final events = await api.events(limit: 30, afterId: _lastEventId);
      for (final event in events.reversed) {
        if (event.id > _lastEventId) _lastEventId = event.id;
        if (event.notificationId == 0) continue;
        await _show(event);
      }
    } catch (_) {}
  }

  Future<void> _show(EventEntry event) => _plugin.show(
    id: event.id,
    title: event.device.isEmpty ? 'XACT ${event.severity}' : event.device,
    body: event.message,
    payload: event.device,
    notificationDetails: _details,
  );

  static const _details = NotificationDetails(
    android: AndroidNotificationDetails(
      'xact_alerts',
      'XACT alerts',
      channelDescription: 'Events delivered by your XACT server',
      importance: Importance.high,
      priority: Priority.high,
    ),
    iOS: DarwinNotificationDetails(),
  );
}
