import 'package:flutter/material.dart';

import '../models/models.dart';
import '../services/notification_service.dart';
import '../services/realtime_service.dart';
import '../services/session_controller.dart';
import '../widgets/common.dart';
import 'dashboard_screen.dart';
import 'device_detail_screen.dart';
import 'device_list_screen.dart';
import 'events_screen.dart';
import 'reports_screen.dart';
import 'settings_screen.dart';

class HomeShell extends StatefulWidget {
  const HomeShell({
    super.key,
    required this.controller,
    required this.notifications,
  });
  final SessionController controller;
  final NotificationService notifications;

  @override
  State<HomeShell> createState() => _HomeShellState();
}

class _HomeShellState extends State<HomeShell> {
  final _deviceKey = GlobalKey<DeviceListScreenState>();
  late final RealtimeService _realtime;
  int _index = 0;

  static const _titles = [
    'Devices',
    'Events',
    'Dashboards',
    'Reports',
    'Settings',
  ];

  @override
  void initState() {
    super.initState();
    _realtime = RealtimeService(widget.controller.api);
    _realtime.connect(widget.controller.session!.user);
    widget.notifications.bindRealtime(_realtime);
    widget.notifications.onDeviceSelected = _openDeviceByName;
    if (widget.controller.notificationsEnabled) widget.notifications.start();
  }

  @override
  void dispose() {
    widget.notifications.onDeviceSelected = null;
    widget.notifications.stop();
    _realtime.dispose();
    super.dispose();
  }

  void _openDevice(Device device) {
    Navigator.of(context).push(
      MaterialPageRoute(
        builder: (_) => DeviceDetailScreen(
          device: device,
          api: widget.controller.api,
          realtime: _realtime,
        ),
      ),
    );
  }

  Future<void> _openDeviceByName(String name) async {
    var device = _deviceKey.currentState?.findDevice(name);
    if (device == null) {
      try {
        final devices = await widget.controller.api.devices();
        final target = name.toLowerCase();
        for (final candidate in devices) {
          if (candidate.name.toLowerCase() == target ||
              candidate.path.toLowerCase() == target ||
              candidate.path.toLowerCase().endsWith('.$target')) {
            device = candidate;
            break;
          }
        }
      } catch (_) {}
    }
    if (device != null && mounted) {
      _openDevice(device);
    } else if (mounted) {
      showMessage(context, 'Device "$name" is not available to this account.');
    }
  }

  @override
  Widget build(BuildContext context) {
    final session = widget.controller.session!;
    final screens = [
      DeviceListScreen(
        key: _deviceKey,
        api: widget.controller.api,
        openDevice: _openDevice,
      ),
      EventsScreen(api: widget.controller.api, openDevice: _openDeviceByName),
      DashboardScreen(
        api: widget.controller.api,
        session: session,
        initialDashboardId: widget.controller.initialDashboardId,
        onDashboardChanged: widget.controller.setInitialDashboard,
      ),
      ReportsScreen(api: widget.controller.api),
      SettingsScreen(
        controller: widget.controller,
        notifications: widget.notifications,
      ),
    ];

    return Scaffold(
      appBar: AppBar(
        title: Text(
          _titles[_index],
          style: const TextStyle(fontWeight: FontWeight.w700),
        ),
        actions: [
          Center(
            child: Container(
              constraints: const BoxConstraints(maxWidth: 135),
              padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 6),
              decoration: BoxDecoration(
                color: Theme.of(
                  context,
                ).colorScheme.primary.withValues(alpha: .1),
                borderRadius: BorderRadius.circular(20),
              ),
              child: Row(
                mainAxisSize: MainAxisSize.min,
                children: [
                  Icon(
                    Icons.apartment_rounded,
                    size: 14,
                    color: Theme.of(context).colorScheme.primary,
                  ),
                  const SizedBox(width: 6),
                  Flexible(
                    child: Text(
                      session.user.tenantId,
                      overflow: TextOverflow.ellipsis,
                      style: const TextStyle(
                        fontSize: 11,
                        fontWeight: FontWeight.w700,
                      ),
                    ),
                  ),
                ],
              ),
            ),
          ),
          const SizedBox(width: 12),
        ],
      ),
      body: IndexedStack(index: _index, children: screens),
      bottomNavigationBar: NavigationBar(
        selectedIndex: _index,
        onDestinationSelected: (value) => setState(() => _index = value),
        destinations: const [
          NavigationDestination(
            icon: Icon(Icons.memory_outlined),
            selectedIcon: Icon(Icons.memory_rounded),
            label: 'Devices',
          ),
          NavigationDestination(
            icon: Icon(Icons.event_note_outlined),
            selectedIcon: Icon(Icons.event_note_rounded),
            label: 'Events',
          ),
          NavigationDestination(
            icon: Icon(Icons.dashboard_outlined),
            selectedIcon: Icon(Icons.dashboard_rounded),
            label: 'Dashboards',
          ),
          NavigationDestination(
            icon: Icon(Icons.picture_as_pdf_outlined),
            selectedIcon: Icon(Icons.picture_as_pdf_rounded),
            label: 'Reports',
          ),
          NavigationDestination(
            icon: Icon(Icons.settings_outlined),
            selectedIcon: Icon(Icons.settings_rounded),
            label: 'Settings',
          ),
        ],
      ),
    );
  }
}
