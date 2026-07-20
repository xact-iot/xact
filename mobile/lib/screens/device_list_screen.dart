import 'package:flutter/material.dart';

import '../models/models.dart';
import '../services/api_client.dart';
import '../theme.dart';
import '../widgets/common.dart';

class DeviceListScreen extends StatefulWidget {
  const DeviceListScreen({
    super.key,
    required this.api,
    required this.openDevice,
  });
  final XactApiClient api;
  final ValueChanged<Device> openDevice;

  @override
  State<DeviceListScreen> createState() => DeviceListScreenState();
}

class DeviceListScreenState extends State<DeviceListScreen> {
  final _search = TextEditingController();
  List<Device> _devices = const [];
  bool _loading = true;
  String? _error;

  List<Device> get devices => _devices;

  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void dispose() {
    _search.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final devices = await widget.api.devices();
      if (mounted) setState(() => _devices = devices);
    } catch (error) {
      if (mounted) setState(() => _error = '$error');
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  Device? findDevice(String value) {
    final target = value.toLowerCase();
    for (final device in _devices) {
      if (device.name.toLowerCase() == target ||
          device.path.toLowerCase() == target ||
          device.path.toLowerCase().endsWith('.$target')) {
        return device;
      }
    }
    return null;
  }

  @override
  Widget build(BuildContext context) {
    if (_loading && _devices.isEmpty) {
      return const LoadingView(label: 'Loading devices');
    }
    if (_error != null && _devices.isEmpty) {
      return ErrorView(message: _error!, onRetry: _load);
    }

    final query = _search.text.trim().toLowerCase();
    final visible = _devices
        .where(
          (device) =>
              query.isEmpty ||
              device.name.toLowerCase().contains(query) ||
              device.location.toLowerCase().contains(query) ||
              device.type.toLowerCase().contains(query),
        )
        .toList();

    return RefreshIndicator(
      onRefresh: _load,
      child: CustomScrollView(
        physics: const AlwaysScrollableScrollPhysics(),
        slivers: [
          SliverToBoxAdapter(
            child: Padding(
              padding: const EdgeInsets.fromLTRB(16, 8, 16, 16),
              child: TextField(
                controller: _search,
                onChanged: (_) => setState(() {}),
                decoration: InputDecoration(
                  hintText: 'Search devices or locations',
                  prefixIcon: const Icon(Icons.search_rounded),
                  suffixIcon: query.isEmpty
                      ? null
                      : IconButton(
                          icon: const Icon(Icons.close_rounded),
                          onPressed: () => setState(_search.clear),
                        ),
                ),
              ),
            ),
          ),
          if (visible.isEmpty)
            const SliverFillRemaining(
              hasScrollBody: false,
              child: EmptyView(
                icon: Icons.sensors_off_outlined,
                title: 'No devices found',
                message: 'No permitted device matches this search.',
              ),
            )
          else
            SliverPadding(
              padding: const EdgeInsets.fromLTRB(16, 0, 16, 24),
              sliver: SliverList.separated(
                itemCount: visible.length,
                separatorBuilder: (_, _) => const SizedBox(height: 10),
                itemBuilder: (context, index) => _DeviceRow(
                  device: visible[index],
                  onTap: () => widget.openDevice(visible[index]),
                ),
              ),
            ),
        ],
      ),
    );
  }
}

class _DeviceRow extends StatelessWidget {
  const _DeviceRow({required this.device, required this.onTap});
  final Device device;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final states = device.kpis
        .map((item) => item.status)
        .where((value) => value.isNotEmpty);
    final status = states.any((value) => statusColor(value) != xactTeal)
        ? states.first
        : 'Online';
    final color = statusColor(status);
    return Card(
      child: InkWell(
        onTap: onTap,
        borderRadius: BorderRadius.circular(18),
        child: Padding(
          padding: const EdgeInsets.all(15),
          child: Row(
            children: [
              Container(
                width: 46,
                height: 46,
                decoration: BoxDecoration(
                  color: color.withValues(alpha: .1),
                  borderRadius: BorderRadius.circular(14),
                ),
                child: Icon(Icons.memory_rounded, color: color),
              ),
              const SizedBox(width: 14),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      device.name,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: const TextStyle(
                        fontWeight: FontWeight.w700,
                        fontSize: 16,
                      ),
                    ),
                    const SizedBox(height: 5),
                    Row(
                      children: [
                        const Icon(
                          Icons.location_on_outlined,
                          size: 14,
                          color: Colors.white38,
                        ),
                        const SizedBox(width: 4),
                        Expanded(
                          child: Text(
                            device.location.isEmpty
                                ? device.type
                                : device.location,
                            maxLines: 1,
                            overflow: TextOverflow.ellipsis,
                            style: TextStyle(
                              fontSize: 12,
                              color: Colors.white.withValues(alpha: .48),
                            ),
                          ),
                        ),
                      ],
                    ),
                  ],
                ),
              ),
              const SizedBox(width: 10),
              Column(
                crossAxisAlignment: CrossAxisAlignment.end,
                children: [
                  Row(
                    children: [
                      Container(
                        width: 7,
                        height: 7,
                        decoration: BoxDecoration(
                          shape: BoxShape.circle,
                          color: color,
                        ),
                      ),
                      const SizedBox(width: 6),
                      Text(
                        status.isEmpty ? 'Online' : status,
                        style: TextStyle(
                          fontSize: 11,
                          fontWeight: FontWeight.w600,
                          color: color,
                        ),
                      ),
                    ],
                  ),
                  const SizedBox(height: 6),
                  Text(
                    '${device.kpis.length} KPI${device.kpis.length == 1 ? '' : 's'}',
                    style: const TextStyle(fontSize: 11, color: Colors.white30),
                  ),
                ],
              ),
              const SizedBox(width: 2),
              const Icon(Icons.chevron_right_rounded, color: Colors.white30),
            ],
          ),
        ),
      ),
    );
  }
}
