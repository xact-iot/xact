import 'package:flutter/material.dart';

import '../models/models.dart';
import '../services/api_client.dart';
import '../widgets/common.dart';
import '../widgets/event_card.dart';

class EventsScreen extends StatefulWidget {
  const EventsScreen({super.key, required this.api, required this.openDevice});
  final XactApiClient api;
  final ValueChanged<String> openDevice;

  @override
  State<EventsScreen> createState() => _EventsScreenState();
}

class _EventsScreenState extends State<EventsScreen> {
  final _scroll = ScrollController();
  List<EventEntry> _events = const [];
  bool _loading = true;
  bool _loadingMore = false;
  String? _error;
  String _severity = 'ALL';

  @override
  void initState() {
    super.initState();
    _scroll.addListener(_onScroll);
    _load();
  }

  @override
  void dispose() {
    _scroll
      ..removeListener(_onScroll)
      ..dispose();
    super.dispose();
  }

  Future<void> _load() async {
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final result = await widget.api.events(limit: 30);
      if (mounted) setState(() => _events = result);
    } catch (error) {
      if (mounted) setState(() => _error = '$error');
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  void _onScroll() {
    if (_scroll.position.extentAfter < 300) _loadMore();
  }

  Future<void> _loadMore() async {
    if (_loadingMore || _events.isEmpty) return;
    _loadingMore = true;
    try {
      final result = await widget.api.events(limit: _events.length + 30);
      if (mounted) setState(() => _events = result);
    } finally {
      _loadingMore = false;
    }
  }

  @override
  Widget build(BuildContext context) {
    if (_loading && _events.isEmpty) {
      return const LoadingView(label: 'Loading events');
    }
    if (_error != null && _events.isEmpty) {
      return ErrorView(message: _error!, onRetry: _load);
    }
    final visible = _severity == 'ALL'
        ? _events
        : _events.where((event) => event.severity == _severity).toList();

    return RefreshIndicator(
      onRefresh: _load,
      child: CustomScrollView(
        controller: _scroll,
        physics: const AlwaysScrollableScrollPhysics(),
        slivers: [
          SliverToBoxAdapter(
            child: SizedBox(
              height: 50,
              child: ListView(
                padding: const EdgeInsets.symmetric(
                  horizontal: 16,
                  vertical: 5,
                ),
                scrollDirection: Axis.horizontal,
                children: [
                  for (final severity in const [
                    'ALL',
                    'CRITICAL',
                    'ERROR',
                    'WARN',
                    'INFO',
                    'DEBUG',
                  ]) ...[
                    ChoiceChip(
                      label: Text(severity),
                      selected: _severity == severity,
                      onSelected: (_) => setState(() => _severity = severity),
                    ),
                    const SizedBox(width: 7),
                  ],
                ],
              ),
            ),
          ),
          if (visible.isEmpty)
            const SliverFillRemaining(
              hasScrollBody: false,
              child: EmptyView(
                icon: Icons.event_note_outlined,
                title: 'No events',
                message: 'There are no permitted events for this filter.',
              ),
            )
          else
            SliverPadding(
              padding: const EdgeInsets.fromLTRB(16, 9, 16, 24),
              sliver: SliverList.separated(
                itemCount: visible.length + (_loadingMore ? 1 : 0),
                separatorBuilder: (_, _) => const SizedBox(height: 9),
                itemBuilder: (context, index) {
                  if (index == visible.length) {
                    return const Padding(
                      padding: EdgeInsets.all(12),
                      child: Center(
                        child: CircularProgressIndicator(strokeWidth: 2),
                      ),
                    );
                  }
                  final event = visible[index];
                  return EventCard(
                    event: event,
                    onTap: event.device.isEmpty
                        ? null
                        : () => widget.openDevice(event.device),
                  );
                },
              ),
            ),
        ],
      ),
    );
  }
}
