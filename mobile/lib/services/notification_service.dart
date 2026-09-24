import 'dart:async';
import 'dart:convert';

import 'package:flutter/foundation.dart';
import 'package:flutter/widgets.dart';
import 'package:flutter_local_notifications/flutter_local_notifications.dart';
import 'package:firebase_core/firebase_core.dart';
import 'package:firebase_messaging/firebase_messaging.dart';
import 'package:flutter/services.dart';

import '../models/models.dart';
import 'push_session.dart';
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
  Map<String, dynamic>? _pendingMessage;
  Map<String, dynamic>? _push;
  AuthSession? _session;
  bool _securePushSupported = false;
  int _generation = 0;
  Future<void>? _registration;

  static const _bootstrapChannel = MethodChannel(
    'com.xact.iot.mobile/firebase_bootstrap',
  );

  set onDeviceSelected(ValueChanged<String>? callback) {
    _onDeviceSelected = callback;
    final pending = _pendingMessage;
    if (callback != null && pending != null) {
      _pendingMessage = null;
      unawaited(_selectMessage(pending));
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
        try {
          final data =
              jsonDecode(response.payload ?? '') as Map<String, dynamic>;
          unawaited(_selectMessage(data));
        } catch (_) {}
      },
    );
    final launch = await _plugin.getNotificationAppLaunchDetails();
    if (launch?.didNotificationLaunchApp ?? false) {
      try {
        final data =
            jsonDecode(launch?.notificationResponse?.payload ?? '')
                as Map<String, dynamic>;
        await _selectMessage(data);
      } catch (_) {}
    }
    if (defaultTargetPlatform == TargetPlatform.android) {
      await _attachExistingFirebase();
    }
  }

  /// Loads this deployment's public Firebase Android options. Returns true
  /// when an app restart is required to replace a different default project.
  Future<bool> configureForServer(String serverUrl) async {
    if (kIsWeb || defaultTargetPlatform != TargetPlatform.android) return false;
    final sessionGeneration = api.generation;
    final config = await api.firebaseConfigForServer(serverUrl);
    api.requireCurrentSession(sessionGeneration);
    _securePushSupported = config.sessionScopedPush;
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
      final session = _session;
      if (_enabled && session != null) await start(session);
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
    final session = _session;
    if (_enabled && session != null) await start(session);
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
    if (await PushSession.read() == null) {
      // Rotate any legacy, automatically displayed notification registration.
      await _messaging!.setAutoInitEnabled(false);
      try {
        await _messaging!.deleteToken().timeout(const Duration(seconds: 5));
      } catch (_) {}
    }
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

  bool _active(int generation) => _enabled && generation == _generation;

  Future<void> start(AuthSession session) async {
    final pending = _pendingMessage;
    stop();
    if (session.isExpired) return;
    _pendingMessage = pending;
    _session = session;
    _enabled = true;
    final generation = _generation;
    final previous = await PushSession.read();
    if (!_active(generation)) return;
    // Keep an existing binding across process restarts, but never across logout.
    if (previous != null &&
        jsonEncode(previous['session']) == jsonEncode(session.toJson())) {
      _push = previous;
    } else {
      _push = await PushSession.create(session);
    }
    if (!_active(generation)) return;
    final messaging = _messaging;
    if (messaging != null && _securePushSupported) {
      await messaging.setAutoInitEnabled(true);
      if (!_active(generation)) return;
      await _tokenSubscription?.cancel();
      _tokenSubscription = messaging.onTokenRefresh.listen((token) {
        if (_active(generation)) _queueRegistration(token, generation);
      });
      unawaited(_registerFirebaseToken(messaging, generation));
    }
    try {
      final latest = await api.events(limit: 1);
      if (!_active(generation)) return;
      _lastEventId = latest.isEmpty ? 0 : latest.first.id;
    } catch (_) {}
    if (_active(generation)) {
      _timer = Timer.periodic(const Duration(seconds: 30), (_) => _poll());
    }
  }

  void _queueRegistration(String token, int generation) {
    final preceding = _registration ?? Future<void>.value();
    _registration = preceding
        .then((_) async {
          if (!_active(generation) || _push == null) return;
          final state = Map<String, dynamic>.from(_push!);
          state['fcmToken'] = token;
          state['projectId'] = _projectId;
          await PushSession.save(state);
          if (!_active(generation)) return;
          _push = state;
          await api.setFcmRegistrationToken(
            token,
            _projectId,
            state['binding'] as String,
          );
        })
        .catchError((Object _) {});
  }

  Future<void> _registerFirebaseToken(
    FirebaseMessaging messaging,
    int generation,
  ) async {
    try {
      final token = await messaging.getToken().timeout(
        const Duration(seconds: 5),
      );
      if (_active(generation) && token != null && token.isNotEmpty) {
        _queueRegistration(token, generation);
      }
    } catch (_) {
      // Polling and the live notification stream remain available.
    }
  }

  Future<void> endSession() async {
    stop();
    await _tokenSubscription?.cancel();
    _tokenSubscription = null;
    // Registration writes must finish before removing the matching server value.
    await _registration;
    final state = await PushSession.read();
    await PushSession.clear();
    _push = null;
    _session = null;
    await _plugin.cancelAll();
    try {
      if (state != null &&
          api.token != null &&
          '${state['fcmToken'] ?? ''}'.isNotEmpty) {
        await api.removeFcmRegistration(
          state['fcmToken'] as String,
          state['binding'] as String,
        );
      }
    } catch (_) {
      // Data-only notifications fail closed against the cleared local binding,
      // including when logout happens offline and the server is unreachable.
    }
    try {
      await _messaging?.setAutoInitEnabled(false);
      await _messaging?.deleteToken().timeout(const Duration(seconds: 5));
    } catch (_) {}
  }

  void bindRealtime(RealtimeService realtime) {
    _realtime = realtime;
    _realtimeSubscription?.cancel();
    _realtimeSubscription = realtime.notifications.listen((notification) {
      if (!_enabled) return;
      final state = _push;
      if (state == null) return;
      unawaited(
        _showData(
          PushSession.payload(
            state,
            device: notification.device,
            title: notification.title,
            body: notification.body,
          ),
        ),
      );
    });
  }

  void stop() {
    _enabled = false;
    _generation++;
    _pendingMessage = null;
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
      final generation = _generation;
      final events = await api.events(limit: 30, afterId: _lastEventId);
      for (final event in events.reversed) {
        if (!_active(generation)) return;
        if (event.id > _lastEventId) _lastEventId = event.id;
        if (event.notificationId == 0) continue;
        await _show(event);
      }
    } catch (_) {}
  }

  Future<void> _show(EventEntry event) async {
    final state = _push;
    if (state == null) return;
    await _showData(
      PushSession.payload(
        state,
        device: event.device,
        title: event.device.isEmpty ? 'XACT ${event.severity}' : event.device,
        body: event.message,
      ),
    );
  }

  Future<void> _showData(Map<String, dynamic> data) async {
    final generation = _generation;
    if (!_active(generation) ||
        !await PushSession.allows(data) ||
        !_active(generation)) {
      return;
    }
    await showPushData(_plugin, data);
    if (!_active(generation)) await _plugin.cancelAll();
  }

  Future<void> _showFirebaseMessage(RemoteMessage message) =>
      _showData(message.data);

  void _openFirebaseMessage(RemoteMessage message) =>
      unawaited(_selectMessage(message.data));

  Future<void> _selectMessage(Map<String, dynamic> data) async {
    final generation = api.generation;
    final notificationGeneration = _generation;
    if (!await PushSession.allows(data) ||
        generation != api.generation ||
        notificationGeneration != _generation) {
      return;
    }
    final device = data['device'];
    if (device is! String || device.isEmpty) return;
    final callback = _onDeviceSelected;
    if (callback == null) {
      _pendingMessage = data;
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
      visibility: NotificationVisibility.private,
    ),
    iOS: DarwinNotificationDetails(),
  );
}

Future<void> showPushData(
  FlutterLocalNotificationsPlugin plugin,
  Map<String, dynamic> data,
) => plugin.show(
  id: DateTime.now().millisecondsSinceEpoch.remainder(2147483647),
  title: '${data['title'] ?? 'XACT alert'}',
  body: '${data['body'] ?? ''}',
  payload: jsonEncode(data),
  notificationDetails: NotificationService._details,
);

@pragma('vm:entry-point')
Future<void> handleBackgroundPush(RemoteMessage message) async {
  WidgetsFlutterBinding.ensureInitialized();
  if (!await PushSession.allows(message.data)) return;
  final plugin = FlutterLocalNotificationsPlugin();
  await plugin.initialize(
    settings: const InitializationSettings(
      android: AndroidInitializationSettings('@mipmap/ic_launcher'),
    ),
  );
  if (!await PushSession.allows(message.data)) return;
  await showPushData(plugin, message.data);
  if (!await PushSession.allows(message.data)) await plugin.cancelAll();
}
