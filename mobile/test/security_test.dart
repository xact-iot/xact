// Security regression tests use only synthetic credentials and isolated fixtures.
// ignore_for_file: depend_on_referenced_packages
import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:webview_flutter_platform_interface/webview_flutter_platform_interface.dart';
import 'package:webview_flutter/webview_flutter.dart' show WebViewController;
import 'package:xact_mobile/models/models.dart';
import 'package:xact_mobile/app.dart';
import 'package:xact_mobile/screens/dashboard_screen.dart';
import 'package:xact_mobile/services/api_client.dart';
import 'package:xact_mobile/services/realtime_service.dart';
import 'package:xact_mobile/services/nats_parser.dart';
import 'package:xact_mobile/services/notification_service.dart';
import 'package:xact_mobile/services/push_session.dart';
import 'package:xact_mobile/services/web_session.dart';
import 'package:stream_channel/stream_channel.dart';
import 'package:web_socket_channel/web_socket_channel.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter_local_notifications/flutter_local_notifications.dart';
import 'package:xact_mobile/services/session_controller.dart';

const _user = XactUser(
  id: 'audit-user',
  username: 'audit',
  tenantId: 'audit-org',
  roles: ['User'],
  allowedOrgs: ['audit-org'],
);

String _token() =>
    'audit.${base64Url.encode(utf8.encode(jsonEncode({'tenant_id': 'audit-org', 'exp': 4102444800})))}.synthetic-signature';

AuthSession _session() => AuthSession(
  serverUrl: 'https://xact.example.test/xact',
  token: _token(),
  user: _user,
);

Future<SessionController> _restored(XactApiClient api) async {
  SharedPreferences.setMockInitialValues({'xact.mobile.notifications': true});
  FlutterSecureStorage.setMockInitialValues({
    'xact.mobile.session': jsonEncode(_session().toJson()),
  });
  final controller = SessionController(api: api);
  await controller.restore();
  return controller;
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  late Directory scratch;
  setUp(() async {
    AndroidFlutterLocalNotificationsPlugin.registerWith();
    scratch = await Directory.systemTemp.createTemp('xact-mobile-security-');
    WebViewPlatform.instance = _AuditWebViewPlatform();
    SharedPreferences.setMockInitialValues({});
    FlutterSecureStorage.setMockInitialValues({});
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(
          const MethodChannel('plugins.flutter.io/path_provider'),
          (call) async =>
              call.method == 'getTemporaryDirectory' ? scratch.path : null,
        );
  });
  tearDown(() async {
    await WebSession.clear();
    await scratch.delete(recursive: true);
    debugDefaultTargetPlatformOverride = null;
  });

  test('external HTTPS APK requests never contain the API bearer', () async {
    final observed = <http.Request>[];
    final api = XactApiClient(
      client: MockClient((r) async {
        observed.add(r);
        return http.Response('synthetic APK', 200);
      }),
    )..configure(serverUrl: _session().serverUrl, token: _token());
    addTearDown(api.close);
    final file = await api.downloadApk(
      const MobileRelease(
        version: '../../unsafe',
        downloadUrl: 'https://downloads.example.test/mobile.apk',
      ),
    );
    expect(observed.single.headers['Authorization'], isNull);
    expect(observed.single.followRedirects, isFalse);
    expect(await file.readAsString(), 'synthetic APK');
    expect(file.path, startsWith('${scratch.path}/xact_exports/'));
    expect(file.path, endsWith('/update.apk'));
  });

  test(
    'APK redirects drop bearer on a foreign host and reject a TLS downgrade',
    () async {
      final requests = <http.Request>[];
      final api = XactApiClient(
        client: MockClient((r) async {
          requests.add(r);
          return http.Response(
            '',
            302,
            headers: {
              'location': requests.length == 1
                  ? 'https://downloads.example.test/app.apk'
                  : 'http://downloads.example.test/app.apk',
            },
          );
        }),
      )..configure(serverUrl: _session().serverUrl, token: _token());
      addTearDown(api.close);
      await expectLater(
        api.downloadApk(
          const MobileRelease(version: '2', downloadUrl: '/api/v1/mobile/apk'),
        ),
        throwsA(isA<XactApiException>()),
      );
      expect(requests.length, 2);
      expect(requests.first.headers['Authorization'], 'Bearer ${_token()}');
      expect(requests.last.headers['Authorization'], isNull);
      expect(
        await Directory('${scratch.path}/xact_exports').list().toList(),
        isEmpty,
      );
    },
  );

  for (final url in [
    'http://xact.example.test',
    'https://user:pw@xact.example.test',
    'https://xact.example.test?redirect=evil',
    'https://xact.example.test#fragment',
  ]) {
    test('rejects unsafe server URL $url before sending a password', () async {
      var requests = 0;
      final api = XactApiClient(
        client: MockClient((_) async {
          requests++;
          return http.Response('{}', 200);
        }),
      );
      addTearDown(api.close);
      expect(XactApiClient.isValidServerUrl(url), isFalse);
      await expectLater(
        api.login(serverUrl: url, username: 'audit', password: 'synthetic'),
        throwsA(isA<XactApiException>()),
      );
      expect(requests, 0);
    });
  }

  test('HTTP APK URL is rejected before any network request', () async {
    var requests = 0;
    final api = XactApiClient(
      client: MockClient((_) async {
        requests++;
        return http.Response('', 200);
      }),
    )..configure(serverUrl: _session().serverUrl, token: _token());
    addTearDown(api.close);
    await expectLater(
      api.downloadApk(
        const MobileRelease(
          version: '2',
          downloadUrl: 'http://downloads.example.test/app.apk',
        ),
      ),
      throwsA(isA<XactApiException>()),
    );
    expect(requests, 0);
  });

  test('authenticated API redirects are not followed', () async {
    var requests = 0;
    final api = XactApiClient(
      client: MockClient((r) async {
        requests++;
        expect(r.followRedirects, isFalse);
        return http.Response(
          '',
          302,
          headers: {'location': 'https://outside.example.test/'},
        );
      }),
    )..configure(serverUrl: _session().serverUrl, token: _token());
    addTearDown(api.close);
    await expectLater(api.reports(), throwsA(isA<XactApiException>()));
    expect(requests, 1);
  });

  test(
    'logout discards a pending report response and clears existing reports',
    () async {
      final pending = Completer<http.Response>();
      var delayed = false;
      final api = XactApiClient(
        client: MockClient(
          (_) async =>
              delayed ? pending.future : http.Response('%PDF-synthetic', 200),
        ),
      );
      final controller = await _restored(api);
      addTearDown(controller.dispose);
      const report = ReportInfo(id: '1', name: 'Audit report', description: '');
      final file = await api.downloadReport(report);
      delayed = true;
      final download = api.downloadReport(report);
      final rejects = expectLater(download, throwsA(isA<XactApiException>()));
      await controller.logout();
      pending.complete(http.Response('%PDF-old-user', 200));
      await rejects;
      expect(await file.exists(), isFalse);
      expect(
        await const FlutterSecureStorage().read(
          key: SessionController.sessionKey,
        ),
        isNull,
      );
      expect(controller.notificationsEnabled, isFalse);
    },
  );

  testWidgets('account teardown removes pushed screens from the navigator', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(390, 844);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    final api = XactApiClient(
      client: MockClient(
        (_) async => http.Response('{"error":"Invalid credentials"}', 401),
      ),
    );
    final controller = SessionController(api: api);
    final notifications = NotificationService(api);
    await tester.runAsync(controller.restore);
    await tester.pumpWidget(
      XactMobileApp(controller: controller, notifications: notifications),
    );
    await tester.pumpAndSettle();
    final navigator = tester.state<NavigatorState>(find.byType(Navigator));
    unawaited(
      navigator.push(
        MaterialPageRoute<void>(
          builder: (_) => const Scaffold(body: Text('Private device details')),
        ),
      ),
    );
    await tester.pumpAndSettle();
    expect(find.text('Private device details'), findsOneWidget);
    await tester.runAsync(controller.logout);
    await tester.pumpAndSettle();
    expect(find.text('Private device details'), findsNothing);
    expect(
      tester.state<NavigatorState>(find.byType(Navigator)).canPop(),
      isFalse,
    );
    await tester.runAsync(() async {
      await expectLater(
        controller.login(_session().serverUrl, 'audit', 'synthetic'),
        throwsA(isA<XactApiException>()),
      );
    });
    await tester.pumpAndSettle();
    expect(find.text('Invalid credentials'), findsOneWidget);
    await tester.pumpWidget(const SizedBox.shrink());
    await notifications.dispose();
    controller.dispose();
  });

  testWidgets(
    'WebView only hands a session to the trusted bootstrap document',
    (tester) async {
      final platform = _AuditWebViewPlatform();
      WebViewPlatform.instance = platform;
      final api = _DashboardApi()
        ..configure(serverUrl: _session().serverUrl, token: _token());
      addTearDown(api.close);
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: DashboardScreen(
              api: api,
              session: _session(),
              initialDashboardId: null,
              onDashboardChanged: (_) {},
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();
      final web = platform.controller;
      final delegate = platform.delegate;
      expect(web.loaded.single, api.dashboardBootstrapUri);
      for (final url in [
        'https://attacker.example.test/landing',
        'http://xact.example.test/xact/',
        'https://xact.example.test:444/xact/',
        'https://xact.example.test/other/',
      ]) {
        expect(
          await delegate.navigationRequested!(
            NavigationRequest(url: url, isMainFrame: true),
          ),
          NavigationDecision.prevent,
        );
        delegate.pageFinished!(url);
      }
      await tester.pumpAndSettle();
      expect(web.scripts, isEmpty);
      delegate.pageFinished!(api.dashboardBootstrapUri.toString());
      await tester.pumpAndSettle();
      expect(web.scripts.single, contains('if (location.href ==='));
      expect(
        web.scripts.single,
        contains("sessionStorage.setItem('xact_auth_token'"),
      );
      expect(web.scripts.single, isNot(contains('localStorage.setItem')));
      expect(
        await delegate.navigationRequested!(
          NavigationRequest(url: api.dashboardUrl(1), isMainFrame: true),
        ),
        NavigationDecision.navigate,
      );
      api.invalidatePendingRequests();
      await WebSession.clear();
      expect(web.storageClears, 1);
      expect(web.cacheClears, 1);
      expect(web.loaded.last.toString(), 'about:blank');
      expect(web.scripts.last, contains('sessionStorage.clear()'));
      expect(platform.cookieClears, greaterThan(0));
      await tester.pumpWidget(const SizedBox.shrink());
    },
  );

  test(
    'browser cleanup still clears storage, cache and cookies if script execution fails',
    () async {
      final platform = _AuditWebViewPlatform();
      WebViewPlatform.instance = platform;
      final controller = WebViewController();
      WebSession.register(controller);
      final web = platform.controller..failScript = true;
      await expectLater(WebSession.clear(), throwsA(isA<StateError>()));
      expect(web.loaded.last.toString(), 'about:blank');
      expect(web.storageClears, 1);
      expect(web.cacheClears, 1);
      expect(platform.cookieClears, 1);
      web.failScript = false;
      await WebSession.clear();
    },
  );

  test(
    'push payloads require the matching account, organisation and unexpired session',
    () async {
      final api = XactApiClient();
      final controller = await _restored(api);
      addTearDown(controller.dispose);
      final state = await PushSession.create(_session());
      final data = PushSession.payload(
        state,
        device: 'pump',
        title: 'Alert',
        body: 'Synthetic',
      );
      expect(await PushSession.allows(data), isTrue);
      for (final change in [
        {'binding': ''},
        {'binding': 'another-session'},
        {'userId': 'another-user'},
        {'orgName': 'another-org'},
      ]) {
        expect(await PushSession.allows({...data, ...change}), isFalse);
      }
      final expired = {
        ..._session().toJson(),
        'token':
            'audit.${base64Url.encode(utf8.encode(jsonEncode({'exp': 1, 'tenant_id': _user.tenantId})))}.synthetic',
      };
      await PushSession.storage.write(
        key: PushSession.sessionKey,
        value: jsonEncode(expired),
      );
      state['session'] = expired;
      await PushSession.save(state);
      expect(await PushSession.allows(data), isFalse);
    },
  );

  test(
    'notification preferences belong to an account, not the device',
    () async {
      final api = XactApiClient(
        client: MockClient((r) async {
          if (r.url.path.endsWith('/login')) {
            return http.Response(
              jsonEncode({
                'token': _token(),
                'user': {..._user.toJson(), 'id': 'second-user'},
              }),
              200,
            );
          }
          return http.Response('{"notificationOptions":{}}', 200);
        }),
      );
      final controller = await _restored(api);
      addTearDown(controller.dispose);
      expect(
        controller.notificationsEnabled,
        isFalse,
      ); // legacy global preference ignored
      await controller.setNotifications(true);
      await controller.logout();
      await controller.login(_session().serverUrl, 'other', 'synthetic');
      expect(controller.notificationsEnabled, isFalse);
    },
  );

  test(
    'logout unregisters the matching push token and rejects queued alerts',
    () async {
      debugDefaultTargetPlatformOverride = TargetPlatform.android;
      final calls = <String>[];
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(
            const MethodChannel('dexterous.com/flutter/local_notifications'),
            (call) async {
              calls.add(call.method);
              return null;
            },
          );
      Map<String, dynamic> options = {};
      final api = XactApiClient(
        client: MockClient((r) async {
          if (r.method == 'PUT') {
            options = Map<String, dynamic>.from(
              jsonDecode(r.body)['notificationOptions'],
            );
          }
          return http.Response(
            jsonEncode({'notificationOptions': options}),
            200,
          );
        }),
      );
      final controller = await _restored(api);
      final notifications = NotificationService(api);
      controller.endNotifications = notifications.endSession;
      final state = await PushSession.create(controller.session!);
      state['fcmToken'] = 'synthetic-device-token';
      await PushSession.save(state);
      options = {
        'fcmEnabled': true,
        'fcmToken': state['fcmToken'],
        'fcmBinding': state['binding'],
      };
      final data = PushSession.payload(
        state,
        device: 'pump',
        title: 'Audit',
        body: 'Synthetic',
      );
      expect(await PushSession.allows(data), isTrue);
      expect(
        await PushSession.allows({...data, 'orgName': 'other-org'}),
        isFalse,
      );
      expect(
        await PushSession.allows({...data, 'userId': 'other-user'}),
        isFalse,
      );
      expect(
        await PushSession.allows({...data, 'binding': 'old-binding'}),
        isFalse,
      );
      await controller.logout();
      expect(options['fcmEnabled'], isFalse);
      expect(options.containsKey('fcmToken'), isFalse);
      expect(await PushSession.allows(data), isFalse);
      expect(await PushSession.read(), isNull);
      expect(calls, contains('cancelAll'));
      await notifications.dispose();
      controller.dispose();
    },
  );

  test(
    'offline logout revokes local push authority even if server deregistration fails',
    () async {
      debugDefaultTargetPlatformOverride = TargetPlatform.android;
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(
            const MethodChannel('dexterous.com/flutter/local_notifications'),
            (_) async => null,
          );
      final api = XactApiClient(
        client: MockClient((_) async => http.Response('', 503)),
      );
      final controller = await _restored(api);
      final notifications = NotificationService(api);
      controller.endNotifications = notifications.endSession;
      final state = await PushSession.create(controller.session!);
      state['fcmToken'] = 'synthetic-token';
      await PushSession.save(state);
      final data = PushSession.payload(
        state,
        device: 'pump',
        title: 'Audit',
        body: 'Synthetic',
      );
      await controller.logout();
      expect(await PushSession.allows(data), isFalse);
      expect(controller.session, isNull);
      await notifications.dispose();
      controller.dispose();
    },
  );

  test('push unregister does not clear another device registration', () async {
    var writes = 0;
    final api = XactApiClient(
      client: MockClient((r) async {
        if (r.method == 'PUT') writes++;
        return http.Response(
          '{"notificationOptions":{"fcmToken":"other-device","fcmBinding":"other-binding"}}',
          200,
        );
      }),
    )..configure(serverUrl: _session().serverUrl, token: _token());
    await api.removeFcmRegistration('this-device', 'this-binding');
    expect(writes, 0);
    api.close();
  });

  for (final frame in [
    'MSG a 1 -1\r\n',
    'MSG a 1 nope\r\n',
    'MSG a 1 1048577\r\n',
    'MSG a 1 1\r\nx!!',
    '-ERR auth failed\r\n',
  ]) {
    test('NATS parser rejects malformed frame ${jsonEncode(frame)}', () {
      final parser = NatsParser(
        onMessage: (_, _) => fail('unexpected message'),
        onControl: (_) {},
      );
      expect(() => parser.add(utf8.encode(frame)), throwsFormatException);
    });
  }

  test(
    'NATS parser handles fragmented UTF-8 payloads and multiple messages',
    () {
      final messages = <String>[];
      final control = <String>[];
      final parser = NatsParser(
        onMessage: (_, bytes) => messages.add(utf8.decode(bytes)),
        onControl: control.add,
      );
      final bytes = utf8.encode('MSG a 1 2\r\né\r\nPING\r\nMSG b 2 0\r\n\r\n');
      for (final byte in bytes) {
        parser.add([byte]);
      }
      expect(messages, ['é', '']);
      expect(control, ['PING']);
      parser.add(
        utf8.encode('${List.filled(10000, 'PING\r\n').join()}MSG c 3 1\r\n'),
      );
      parser.add(utf8.encode('x\r\n'));
      expect(control.length, 10001);
      expect(messages, ['é', '', 'x']);
      expect(
        () => parser.add(List.filled(NatsParser.maxHeader + 1, 65)),
        throwsFormatException,
      );
    },
  );

  test(
    'NATS refuses WS and disconnects cleanly on malformed framing',
    () async {
      var url = 'ws://broker.example.test';
      var connections = 0;
      final channel = _FakeChannel();
      final api = XactApiClient(
        client: MockClient(
          (_) async => http.Response(
            jsonEncode({
              'natsWsUrl': url,
              'username': 'browser',
              'password': _token(),
            }),
            200,
          ),
        ),
      )..configure(serverUrl: _session().serverUrl, token: _token());
      final realtime = RealtimeService(
        api,
        connectChannel: (_) {
          connections++;
          return channel;
        },
      );
      await realtime.connect(_user);
      expect(connections, 0);
      url = 'wss://broker.example.test';
      await realtime.connect(_user);
      expect(channel.sink.sent.first, contains('"tls_required":true'));
      channel.incoming.add('PONG\r\n');
      await Future<void>.delayed(Duration.zero);
      expect(realtime.connected, isTrue);
      channel.incoming.add('MSG a 1 -1\r\n\r\n');
      await Future<void>.delayed(Duration.zero);
      expect(realtime.connected, isFalse);
      expect(channel.sink.closed, isTrue);
      await realtime.dispose();
      await channel.incoming.close();
      api.close();
    },
  );

  test(
    'disposing NATS while configuration is pending cannot reconnect',
    () async {
      final reply = Completer<http.Response>();
      var connections = 0;
      final api = XactApiClient(client: MockClient((_) => reply.future))
        ..configure(serverUrl: _session().serverUrl, token: _token());
      final realtime = RealtimeService(
        api,
        connectChannel: (_) {
          connections++;
          return _FakeChannel();
        },
      );
      final connecting = realtime.connect(_user);
      await Future<void>.delayed(Duration.zero);
      await realtime.dispose();
      reply.complete(
        http.Response('{"natsWsUrl":"wss://broker.example.test"}', 200),
      );
      await connecting;
      expect(connections, 0);
      api.close();
    },
  );
}

class _FakeChannel extends StreamChannelMixin<dynamic>
    implements WebSocketChannel {
  final incoming = StreamController<dynamic>();
  @override
  Stream<dynamic> get stream => incoming.stream;
  @override
  final _FakeSink sink = _FakeSink();
  @override
  Future<void> get ready => Future.value();
  @override
  String? get protocol => 'nats';
  @override
  int? get closeCode => null;
  @override
  String? get closeReason => null;
}

class _FakeSink implements WebSocketSink {
  final sent = <dynamic>[];
  bool closed = false;
  @override
  void add(dynamic value) => sent.add(value);
  @override
  void addError(Object error, [StackTrace? stackTrace]) {}
  @override
  Future<void> addStream(Stream<dynamic> stream) async {
    await for (final value in stream) {
      add(value);
    }
  }

  @override
  Future<void> close([int? code, String? reason]) async {
    closed = true;
  }

  @override
  Future<void> get done => Future.value();
}

class _DashboardApi extends XactApiClient {
  @override
  Future<List<DashboardInfo>> dashboards() async => [
    DashboardInfo.fromJson({'id': 1, 'name': 'Audit dashboard'}),
  ];
  @override
  Future<MobileAppConfig> mobileAppConfig() async => const MobileAppConfig();
}

class _AuditWebViewPlatform extends WebViewPlatform {
  late _AuditWebViewController controller;
  late _AuditNavigationDelegate delegate;
  int cookieClears = 0;
  @override
  PlatformWebViewController createPlatformWebViewController(
    PlatformWebViewControllerCreationParams params,
  ) => controller = _AuditWebViewController(params);
  @override
  PlatformNavigationDelegate createPlatformNavigationDelegate(
    PlatformNavigationDelegateCreationParams params,
  ) => delegate = _AuditNavigationDelegate(params);
  @override
  PlatformWebViewWidget createPlatformWebViewWidget(
    PlatformWebViewWidgetCreationParams params,
  ) => _AuditWebViewWidget(params);
  @override
  PlatformWebViewCookieManager createPlatformCookieManager(
    PlatformWebViewCookieManagerCreationParams params,
  ) => _AuditCookieManager(params, this);
}

class _AuditWebViewController extends PlatformWebViewController {
  _AuditWebViewController(super.params) : super.implementation();
  final scripts = <String>[];
  final loaded = <Uri>[];
  int storageClears = 0;
  int cacheClears = 0;
  bool failScript = false;
  @override
  Future<void> setJavaScriptMode(JavaScriptMode mode) async {}
  @override
  Future<void> setBackgroundColor(Color color) async {}
  @override
  Future<void> setPlatformNavigationDelegate(
    PlatformNavigationDelegate handler,
  ) async {}
  @override
  Future<void> runJavaScript(String script) async {
    if (failScript) throw StateError('Synthetic browser failure');
    scripts.add(script);
  }

  @override
  Future<void> loadRequest(LoadRequestParams params) async {
    loaded.add(params.uri);
  }

  @override
  Future<void> clearLocalStorage() async {
    storageClears++;
  }

  @override
  Future<void> clearCache() async {
    cacheClears++;
  }
}

class _AuditNavigationDelegate extends PlatformNavigationDelegate {
  _AuditNavigationDelegate(super.params) : super.implementation();
  PageEventCallback? pageFinished;
  NavigationRequestCallback? navigationRequested;
  @override
  Future<void> setOnPageFinished(PageEventCallback callback) async {
    pageFinished = callback;
  }

  @override
  Future<void> setOnProgress(ProgressCallback callback) async {}
  @override
  Future<void> setOnNavigationRequest(
    NavigationRequestCallback callback,
  ) async {
    navigationRequested = callback;
  }
}

class _AuditWebViewWidget extends PlatformWebViewWidget {
  _AuditWebViewWidget(super.params) : super.implementation();
  @override
  Widget build(BuildContext context) => const SizedBox.shrink();
}

class _AuditCookieManager extends PlatformWebViewCookieManager {
  _AuditCookieManager(super.params, this.platform) : super.implementation();
  final _AuditWebViewPlatform platform;
  @override
  Future<bool> clearCookies() async {
    platform.cookieClears++;
    return true;
  }
}
