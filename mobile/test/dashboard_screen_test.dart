import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:xact_mobile/models/models.dart';
import 'package:xact_mobile/screens/dashboard_screen.dart';
import 'package:xact_mobile/services/api_client.dart';

void main() {
  testWidgets('dashboard retry clears a previous connection error', (
    tester,
  ) async {
    final api = _RecoveringDashboardApi();
    const session = AuthSession(
      serverUrl: 'https://xact.example.com/xact',
      token: 'token',
      user: XactUser(
        id: '1',
        username: 'operator',
        tenantId: 'default',
        roles: ['User'],
        allowedOrgs: ['default'],
      ),
    );

    await tester.pumpWidget(
      MaterialApp(
        home: DashboardScreen(
          api: api,
          session: session,
          initialDashboardId: null,
          onDashboardChanged: (_) {},
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('Could not reach the XACT server.'), findsOneWidget);
    await tester.tap(find.text('Try again'));
    await tester.pumpAndSettle();

    expect(find.text('Could not reach the XACT server.'), findsNothing);
    expect(find.text('No dashboards'), findsOneWidget);
  });
}

class _RecoveringDashboardApi extends XactApiClient {
  var calls = 0;

  @override
  Future<List<DashboardInfo>> dashboards() async {
    calls++;
    if (calls == 1) {
      throw const XactApiException('Could not reach the XACT server.');
    }
    return const [];
  }

  @override
  Future<MobileAppConfig> mobileAppConfig() async => const MobileAppConfig();
}
