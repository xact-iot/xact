import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:xact_mobile/models/models.dart';
import 'package:xact_mobile/services/api_client.dart';

void main() {
  test('normalizes a bare XACT server host', () {
    expect(
      XactApiClient.normalizeServerUrl('xact.example.com/'),
      'https://xact.example.com/xact',
    );
    expect(
      XactApiClient.normalizeServerUrl('https://xact.example.com/xact/'),
      'https://xact.example.com/xact',
    );
  });

  test('validates XACT server URLs', () {
    expect(XactApiClient.isValidServerUrl('xact.example.com'), isTrue);
    expect(XactApiClient.isValidServerUrl('http://10.0.2.2:8080/xact'), isTrue);
    expect(XactApiClient.isValidServerUrl('ftp://xact.example.com'), isFalse);
    expect(XactApiClient.isValidServerUrl('not a host'), isFalse);
    expect(XactApiClient.isValidServerUrl(''), isFalse);
  });

  test('uses dashboard-only presentation for embedded dashboards', () {
    final api = XactApiClient()
      ..configure(serverUrl: 'http://10.0.2.2:8080/xact', token: 'token');
    expect(
      api.dashboardUrl(76),
      'http://10.0.2.2:8080/xact/?embedded=dashboard#76',
    );
    api.close();
  });

  test('discovers KPI values from a device tree', () {
    final node = TreeItem(
      name: 'pump-1',
      kind: 'node',
      path: 'default.pumps.pump-1',
      children: [
        TreeItem(
          name: 'meta',
          kind: 'node',
          path: 'default.pumps.pump-1.meta',
          children: [
            TreeItem(
              name: 'name',
              kind: 'leaf',
              path: 'default.pumps.pump-1.meta.name',
              value: 'Main pump',
            ),
          ],
        ),
        TreeItem(
          name: 'kpi',
          kind: 'node',
          path: 'default.pumps.pump-1.kpi',
          children: [
            TreeItem(
              name: 'pressure',
              kind: 'leaf',
              path: 'default.pumps.pump-1.kpi.pressure',
              value: 4.2,
              units: 'bar',
            ),
          ],
        ),
      ],
    );
    final device = Device(path: node.path, node: node);
    expect(device.name, 'Main pump');
    expect(device.kpis.single.value, 4.2);
  });

  test('parses organisation mobile app configuration', () {
    final config = MobileAppConfig.fromJson({
      'deviceParentNodes': ['Pumps', 'Meters'],
      'defaultDashboardName': 'Overview',
    });
    expect(config.deviceParentNodes, ['Pumps', 'Meters']);
    expect(config.defaultDashboardName, 'Overview');
  });

  test('parses runtime Firebase client configuration', () {
    final config = FirebaseClientConfig.fromJson({
      'configured': true,
      'projectId': 'xact-server',
      'appId': '1:123:android:abc',
      'apiKey': 'AIza-public',
      'messagingSenderId': '123',
    });
    expect(config.isComplete, isTrue);
    expect(config.projectId, 'xact-server');
    expect(config.toJson()['messagingSenderId'], '123');
    expect(
      FirebaseClientConfig.fromJson({'configured': false}).isComplete,
      isFalse,
    );
  });

  test('loads Firebase configuration before authentication', () async {
    final api = XactApiClient(
      client: MockClient((request) async {
        expect(
          request.url.toString(),
          'https://xact.example.com/xact/api/v1/mobile/firebase-config',
        );
        expect(request.headers['authorization'], isNull);
        return http.Response(
          '{"configured":true,"projectId":"deployment","appId":"app","apiKey":"key","messagingSenderId":"123"}',
          200,
        );
      }),
    );
    final config = await api.firebaseConfigForServer(
      'https://xact.example.com/xact',
    );
    expect(config.isComplete, isTrue);
    expect(config.projectId, 'deployment');
    api.close();
  });

  test('discovers a root deployment after an HTML proxy-path response', () async {
    var calls = 0;
    final api = XactApiClient(
      client: MockClient((request) async {
        calls++;
        if (request.url.path.startsWith('/xact/')) {
          return http.Response('<html>not this deployment</html>', 200);
        }
        return http.Response(
          '{"configured":true,"projectId":"root","appId":"app","apiKey":"key","messagingSenderId":"123"}',
          200,
        );
      }),
    );
    final config = await api.firebaseConfigForServer('xact.example.com');
    expect(config.projectId, 'root');
    expect(calls, 2);
    api.close();
  });

  test('retries a transient GET connection failure', () async {
    var calls = 0;
    final api = XactApiClient(
      client: MockClient((request) async {
        calls++;
        if (calls == 1) throw const SocketException('temporary failure');
        return http.Response('[]', 200);
      }),
    )..configure(serverUrl: 'https://xact.example.com/xact', token: 'token');

    expect(await api.reports(), isEmpty);
    expect(calls, 2);
    api.close();
  });

  test('queries events with the tenant-relative device path', () async {
    late Uri requestedUrl;
    final payload = base64Url.encode(utf8.encode('{"tenant_id":"default"}'));
    final api =
        XactApiClient(
          client: MockClient((request) async {
            requestedUrl = request.url;
            return http.Response('[]', 200);
          }),
        )..configure(
          serverUrl: 'https://xact.example.com/xact',
          token: 'header.$payload.signature',
        );

    await api.events(
      limit: 10,
      device: 'default.LA_LongBeach.AirQuality.AQ-B-0001',
    );

    expect(
      requestedUrl.queryParameters['device'],
      'LA_LongBeach.AirQuality.AQ-B-0001',
    );
    api.close();
  });
}
