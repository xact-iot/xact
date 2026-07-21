import 'dart:async';

import 'package:flutter/material.dart';

import '../models/models.dart';
import '../services/api_client.dart';
import '../services/realtime_service.dart';
import '../theme.dart';
import '../widgets/common.dart';
import '../widgets/event_card.dart';

class DeviceDetailScreen extends StatefulWidget {
  const DeviceDetailScreen({
    super.key,
    required this.device,
    required this.api,
    required this.realtime,
  });
  final Device device;
  final XactApiClient api;
  final RealtimeService realtime;

  @override
  State<DeviceDetailScreen> createState() => _DeviceDetailScreenState();
}

class _DeviceDetailScreenState extends State<DeviceDetailScreen> {
  late Device _device;
  List<EventEntry> _events = const [];
  bool _loading = true;
  bool _loadingMore = false;
  StreamSubscription<TagUpdate>? _updates;
  final _scroll = ScrollController();

  @override
  void initState() {
    super.initState();
    _device = widget.device;
    _updates = widget.realtime.updates.listen(_applyUpdate);
    _scroll.addListener(_onScroll);
    _refresh();
  }

  @override
  void dispose() {
    _updates?.cancel();
    _scroll
      ..removeListener(_onScroll)
      ..dispose();
    super.dispose();
  }

  void _applyUpdate(TagUpdate update) {
    for (final kpi in _device.kpis) {
      if (kpi.path == update.path || update.path.endsWith(kpi.path)) {
        setState(() {
          kpi.value = update.value;
          kpi.status = update.status;
          kpi.timestamp = update.timestamp;
        });
        return;
      }
    }
  }

  Future<void> _refresh() async {
    try {
      final results = await Future.wait<dynamic>([
        widget.api.device(_device.path),
        widget.api.events(limit: 10, device: _device.path),
      ]);
      if (mounted) {
        setState(() {
          _device = results[0] as Device;
          _events = results[1] as List<EventEntry>;
        });
      }
    } catch (error) {
      if (mounted) showMessage(context, '$error');
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  void _onScroll() {
    if (_scroll.position.extentAfter < 240) _loadMore();
  }

  Future<void> _loadMore() async {
    if (_loadingMore || _events.isEmpty) return;
    _loadingMore = true;
    try {
      final older = await widget.api.events(
        limit: _events.length + 10,
        device: _device.path,
      );
      if (mounted) setState(() => _events = older);
    } finally {
      _loadingMore = false;
    }
  }

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(title: const Text('Device overview')),
    body: _loading
        ? const LoadingView(label: 'Loading device KPIs')
        : RefreshIndicator(
            onRefresh: _refresh,
            child: ListView(
              controller: _scroll,
              physics: const AlwaysScrollableScrollPhysics(),
              padding: const EdgeInsets.fromLTRB(16, 4, 16, 28),
              children: [
                _DeviceHeader(device: _device, live: widget.realtime.connected),
                const SizedBox(height: 22),
                _SectionTitle(
                  label: 'Key performance indicators',
                  count: _device.kpis.length,
                ),
                const SizedBox(height: 10),
                if (_device.kpis.isEmpty)
                  const SizedBox(
                    height: 180,
                    child: EmptyView(
                      icon: Icons.speed_outlined,
                      title: 'No KPI tags',
                      message: 'This device does not expose any KPI tags.',
                    ),
                  )
                else
                  Card(
                    child: Column(
                      children: [
                        for (var i = 0; i < _device.kpis.length; i++) ...[
                          _KpiRow(item: _device.kpis[i]),
                          if (i < _device.kpis.length - 1)
                            Divider(
                              height: 1,
                              indent: 16,
                              endIndent: 16,
                              color: Colors.white.withValues(alpha: .06),
                            ),
                        ],
                      ],
                    ),
                  ),
                const SizedBox(height: 24),
                _SectionTitle(label: 'Recent events', count: _events.length),
                const SizedBox(height: 10),
                if (_events.isEmpty)
                  const SizedBox(
                    height: 160,
                    child: EmptyView(
                      icon: Icons.event_available_outlined,
                      title: 'No device events',
                      message:
                          'No event records are available for this device.',
                    ),
                  )
                else
                  for (final event in _events) ...[
                    EventCard(event: event),
                    const SizedBox(height: 9),
                  ],
                if (_loadingMore)
                  const Padding(
                    padding: EdgeInsets.all(12),
                    child: Center(
                      child: CircularProgressIndicator(strokeWidth: 2),
                    ),
                  ),
              ],
            ),
          ),
  );
}

class _DeviceHeader extends StatelessWidget {
  const _DeviceHeader({required this.device, required this.live});
  final Device device;
  final bool live;

  @override
  Widget build(BuildContext context) => Container(
    padding: const EdgeInsets.all(20),
    decoration: BoxDecoration(
      borderRadius: BorderRadius.circular(22),
      gradient: LinearGradient(
        colors: [
          xactTeal.withValues(alpha: .16),
          xactBlue.withValues(alpha: .06),
        ],
      ),
      border: Border.all(color: xactTeal.withValues(alpha: .18)),
    ),
    child: Row(
      children: [
        Container(
          width: 58,
          height: 58,
          decoration: BoxDecoration(
            color: xactTeal.withValues(alpha: .12),
            borderRadius: BorderRadius.circular(18),
          ),
          child: const Icon(Icons.sensors_rounded, size: 30, color: xactTeal),
        ),
        const SizedBox(width: 16),
        Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(
                device.name,
                style: Theme.of(
                  context,
                ).textTheme.titleLarge?.copyWith(fontWeight: FontWeight.w800),
              ),
              const SizedBox(height: 6),
              Text(
                device.location.isEmpty ? device.path : device.location,
                maxLines: 2,
                overflow: TextOverflow.ellipsis,
                style: TextStyle(color: Colors.white.withValues(alpha: .55)),
              ),
            ],
          ),
        ),
        Container(
          padding: const EdgeInsets.symmetric(horizontal: 9, vertical: 6),
          decoration: BoxDecoration(
            color: (live ? xactTeal : Colors.white38).withValues(alpha: .1),
            borderRadius: BorderRadius.circular(20),
          ),
          child: Row(
            children: [
              Container(
                width: 6,
                height: 6,
                decoration: BoxDecoration(
                  shape: BoxShape.circle,
                  color: live ? xactTeal : Colors.white38,
                ),
              ),
              const SizedBox(width: 6),
              Text(
                live ? 'LIVE' : 'REST',
                style: TextStyle(
                  fontSize: 10,
                  fontWeight: FontWeight.w800,
                  color: live ? xactTeal : Colors.white54,
                ),
              ),
            ],
          ),
        ),
      ],
    ),
  );
}

class _SectionTitle extends StatelessWidget {
  const _SectionTitle({required this.label, required this.count});
  final String label;
  final int count;

  @override
  Widget build(BuildContext context) => Row(
    children: [
      Text(
        label.toUpperCase(),
        style: const TextStyle(
          fontSize: 11,
          fontWeight: FontWeight.w800,
          letterSpacing: 1.1,
          color: xactTeal,
        ),
      ),
      const SizedBox(width: 9),
      Text(
        '$count',
        style: const TextStyle(fontSize: 11, color: Colors.white38),
      ),
    ],
  );
}

class _KpiRow extends StatelessWidget {
  const _KpiRow({required this.item});
  final TreeItem item;

  @override
  Widget build(BuildContext context) {
    final color = statusColor(item.status);
    final label = item.description.isNotEmpty
        ? item.description
        : _prettify(item.name);
    final value = item.value == null
        ? '—'
        : '${item.value}${item.units.isEmpty ? '' : ' ${item.units}'}';
    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 14),
      child: Row(
        children: [
          Container(
            width: 8,
            height: 8,
            decoration: BoxDecoration(shape: BoxShape.circle, color: color),
          ),
          const SizedBox(width: 12),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  label,
                  style: const TextStyle(
                    fontSize: 13,
                    fontWeight: FontWeight.w600,
                  ),
                ),
                if (item.timestamp != null) ...[
                  const SizedBox(height: 3),
                  Text(
                    formatTimestamp(item.timestamp!),
                    style: const TextStyle(fontSize: 10, color: Colors.white30),
                  ),
                ],
              ],
            ),
          ),
          const SizedBox(width: 12),
          Flexible(
            child: Text(
              value,
              textAlign: TextAlign.right,
              style: TextStyle(
                fontSize: 16,
                fontWeight: FontWeight.w800,
                color: color,
              ),
            ),
          ),
        ],
      ),
    );
  }

  String _prettify(String value) => value
      .replaceAll('_', ' ')
      .replaceAllMapped(
        RegExp(r'([a-z])([A-Z])'),
        (match) => '${match[1]} ${match[2]}',
      )
      .split(' ')
      .map(
        (part) => part.isEmpty
            ? part
            : '${part[0].toUpperCase()}${part.substring(1)}',
      )
      .join(' ');
}
