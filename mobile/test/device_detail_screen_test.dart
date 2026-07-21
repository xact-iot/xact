import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:xact_mobile/models/models.dart';
import 'package:xact_mobile/screens/device_detail_screen.dart';
import 'package:xact_mobile/services/api_client.dart';
import 'package:xact_mobile/services/realtime_service.dart';

void main() {
  testWidgets('loads recent events using the full device path', (tester) async {
    final device = Device(
      path: 'default.LA_LongBeach.AirQuality.AQ-B-0001',
      node: TreeItem(
        name: 'AQ-B-0001',
        kind: 'node',
        path: 'default.LA_LongBeach.AirQuality.AQ-B-0001',
        children: [
          TreeItem(
            name: 'kpi',
            kind: 'node',
            path: 'default.LA_LongBeach.AirQuality.AQ-B-0001.kpi',
            children: [
              TreeItem(
                name: 'aqi',
                kind: 'leaf',
                path: 'default.LA_LongBeach.AirQuality.AQ-B-0001.kpi.aqi',
                value: 310,
              ),
            ],
          ),
        ],
      ),
    );
    final api = _RecordingDeviceApi(device);
    final realtime = RealtimeService(api);

    await tester.pumpWidget(
      MaterialApp(
        home: DeviceDetailScreen(device: device, api: api, realtime: realtime),
      ),
    );
    await tester.pumpAndSettle();

    expect(api.requestedDevice, 'default.LA_LongBeach.AirQuality.AQ-B-0001');
    expect(find.text('Composite air quality index high limit'), findsOneWidget);

    await tester.pumpWidget(const SizedBox());
    await tester.pumpAndSettle();
    await realtime.dispose();
  });
}

class _RecordingDeviceApi extends XactApiClient {
  _RecordingDeviceApi(this.deviceResult);

  final Device deviceResult;
  String? requestedDevice;

  @override
  Future<Device> device(String relativePath) async => deviceResult;

  @override
  Future<List<EventEntry>> events({
    int limit = 30,
    String? device,
    int? afterId,
  }) async {
    requestedDevice = device;
    return [
      EventEntry(
        id: 1,
        timestamp: DateTime.now(),
        severity: 'INFO',
        device: 'LA_LongBeach.AirQuality.AQ-B-0001',
        message: 'Composite air quality index high limit',
      ),
    ];
  }
}
