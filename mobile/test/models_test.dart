import 'package:flutter_test/flutter_test.dart';
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
}
