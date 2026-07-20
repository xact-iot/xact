import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter_local_notifications/flutter_local_notifications.dart';
import 'package:firebase_core/firebase_core.dart';
import 'package:firebase_messaging/firebase_messaging.dart';
import 'package:flutter/services.dart';

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
  StreamSubscription<String>? _tokenSubscription;
  StreamSubscription<RemoteMessage>? _messageSubscription;
  StreamSubscription<RemoteMessage>? _openedSubscription;
  RealtimeService? _realtime;
  FirebaseMessaging? _messaging;
  String _projectId = '';
  bool _enabled = false;
  int _lastEventId = 0;
  ValueChanged<String>? _onDeviceSelected;
  String? _pendingDevice;

  static const _bootstrapChannel = MethodChannel(
    'com.xact.iot.mobile/firebase_bootstrap',
  );

  set onDeviceSelected(ValueChanged<String>? callback) {
    _onDeviceSelected = callback;
    final pending = _pendingDevice;
    if (callback != null && pending != null && pending.isNotEmpty) {
      _pendingDevice = null;
      callback(pending);
    }
  }

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
        if (device != null && device.isNotEmpty) _selectDevice(device);
      },
    );
    if (defaultTargetPlatform == TargetPlatform.android) {
      await _attachExistingFirebase();
    }
  }

  /// Loads this deployment's public Firebase Android options. Returns true
  /// when an app restart is required to replace a different default project.
  Future<bool> configureForServer(String serverUrl) async {
    if (kIsWeb || defaultTargetPlatform != TargetPlatform.android) return false;
    final config = await api.firebaseConfigForServer(serverUrl);
    final apps = Firebase.apps;
    if (!config.isComplete) {
      await _bootstrapChannel.invokeMethod<void>('clearConfig');
      return apps.isNotEmpty;
    }

    await _bootstrapChannel.invokeMethod<void>('storeConfig', config.toJson());
    if (apps.isNotEmpty) {
      final current = apps.first.options;
      final changed =
          current.projectId != config.projectId ||
          current.appId != config.appId ||
          current.apiKey != config.apiKey ||
          current.messagingSenderId != config.messagingSenderId;
      if (changed) return true;
      await _attachExistingFirebase();
      return false;
    }

    await Firebase.initializeApp(
      options: FirebaseOptions(
        apiKey: config.apiKey,
        appId: config.appId,
        messagingSenderId: config.messagingSenderId,
        projectId: config.projectId,
      ),
    );
    await _attachExistingFirebase();
    return false;
  }

  Future<void> restartForFirebaseConfig() async {
    if (!kIsWeb && defaultTargetPlatform == TargetPlatform.android) {
      await _bootstrapChannel.invokeMethod<void>('restartApp');
    }
  }

  Future<void> _attachExistingFirebase() async {
    if (Firebase.apps.isEmpty) return;
    _projectId = Firebase.app().options.projectId;
    _messaging = FirebaseMessaging.instance;
    if (_messageSubscription == null) {
      _messageSubscription = FirebaseMessaging.onMessage.listen(
        _showFirebaseMessage,
      );
      _openedSubscription = FirebaseMessaging.onMessageOpenedApp.listen(
        _openFirebaseMessage,
      );
      unawaited(_loadInitialFirebaseMessage());
    }
  }

  Future<void> _loadInitialFirebaseMessage() async {
    try {
      final initial = await _messaging?.getInitialMessage().timeout(
        const Duration(seconds: 3),
      );
      if (initial != null) _openFirebaseMessage(initial);
    } catch (error) {
      debugPrint('Firebase initial message is unavailable: $error');
    }
  }

  Future<void> start() async {
    stop();
    _enabled = true;
    final messaging = _messaging;
    if (messaging != null) {
      await _tokenSubscription?.cancel();
      _tokenSubscription = messaging.onTokenRefresh.listen((token) {
        if (_enabled) {
          api.setFcmRegistrationToken(token, _projectId).catchError((_) {});
        }
      });
      unawaited(_registerFirebaseToken(messaging));
    }
    try {
      final latest = await api.events(limit: 1);
      if (latest.isNotEmpty) _lastEventId = latest.first.id;
    } catch (_) {}
    _timer = Timer.periodic(const Duration(seconds: 30), (_) => _poll());
  }

  Future<void> _registerFirebaseToken(FirebaseMessaging messaging) async {
    try {
      final token = await messaging.getToken().timeout(
        const Duration(seconds: 5),
      );
      if (_enabled && token != null && token.isNotEmpty) {
        await api.setFcmRegistrationToken(token, _projectId);
      }
    } catch (error) {
      debugPrint(
        'Firebase token registration is unavailable; continuing without push: $error',
      );
    }
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
    await _tokenSubscription?.cancel();
    await _messageSubscription?.cancel();
    await _openedSubscription?.cancel();
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

  Future<void> _showFirebaseMessage(RemoteMessage message) async {
    if (!_enabled) return;
    final notification = message.notification;
    await _plugin.show(
      id:
          message.messageId?.hashCode ??
          DateTime.now().millisecondsSinceEpoch.remainder(2147483647),
      title: notification?.title ?? 'XACT alert',
      body: notification?.body ?? '',
      payload: message.data['device'],
      notificationDetails: _details,
    );
  }

  void _openFirebaseMessage(RemoteMessage message) {
    final device = message.data['device'];
    if (device != null && device.isNotEmpty) _selectDevice(device);
  }

  void _selectDevice(String device) {
    final callback = _onDeviceSelected;
    if (callback == null) {
      _pendingDevice = device;
    } else {
      callback(device);
    }
  }

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
