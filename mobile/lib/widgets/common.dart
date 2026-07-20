import 'package:flutter/material.dart';

import '../theme.dart';

class XactBrand extends StatelessWidget {
  const XactBrand({super.key, this.compact = false});
  final bool compact;

  @override
  Widget build(BuildContext context) => Row(
    mainAxisSize: MainAxisSize.min,
    children: [
      Container(
        width: compact ? 32 : 46,
        height: compact ? 32 : 46,
        decoration: BoxDecoration(
          borderRadius: BorderRadius.circular(compact ? 10 : 14),
          gradient: const LinearGradient(colors: [xactTeal, xactBlue]),
          boxShadow: [
            BoxShadow(color: xactTeal.withValues(alpha: .22), blurRadius: 20),
          ],
        ),
        alignment: Alignment.center,
        child: Text(
          'X',
          style: TextStyle(
            color: xactNavy,
            fontWeight: FontWeight.w900,
            fontSize: compact ? 18 : 27,
          ),
        ),
      ),
      const SizedBox(width: 12),
      Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        mainAxisSize: MainAxisSize.min,
        children: [
          Text(
            'XACT',
            style: Theme.of(context).textTheme.titleLarge?.copyWith(
              fontWeight: FontWeight.w800,
              letterSpacing: 2.5,
            ),
          ),
          if (!compact)
            Text(
              'OPERATIONS',
              style: Theme.of(context).textTheme.labelSmall?.copyWith(
                color: xactTeal,
                letterSpacing: 2,
              ),
            ),
        ],
      ),
    ],
  );
}

class LoadingView extends StatelessWidget {
  const LoadingView({super.key, this.label = 'Loading'});
  final String label;

  @override
  Widget build(BuildContext context) => Center(
    child: Column(
      mainAxisSize: MainAxisSize.min,
      children: [
        const CircularProgressIndicator(strokeWidth: 2),
        const SizedBox(height: 16),
        Text(
          label,
          style: TextStyle(color: Colors.white.withValues(alpha: .6)),
        ),
      ],
    ),
  );
}

class ErrorView extends StatelessWidget {
  const ErrorView({super.key, required this.message, required this.onRetry});
  final String message;
  final VoidCallback onRetry;

  @override
  Widget build(BuildContext context) => Center(
    child: Padding(
      padding: const EdgeInsets.all(32),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          const Icon(Icons.cloud_off_rounded, size: 42, color: Colors.white54),
          const SizedBox(height: 14),
          Text(message, textAlign: TextAlign.center),
          const SizedBox(height: 18),
          FilledButton.tonalIcon(
            onPressed: onRetry,
            icon: const Icon(Icons.refresh_rounded),
            label: const Text('Try again'),
          ),
        ],
      ),
    ),
  );
}

class EmptyView extends StatelessWidget {
  const EmptyView({
    super.key,
    required this.icon,
    required this.title,
    required this.message,
  });
  final IconData icon;
  final String title;
  final String message;

  @override
  Widget build(BuildContext context) => Center(
    child: Padding(
      padding: const EdgeInsets.all(36),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(icon, size: 46, color: xactTeal.withValues(alpha: .65)),
          const SizedBox(height: 16),
          Text(title, style: Theme.of(context).textTheme.titleMedium),
          const SizedBox(height: 7),
          Text(
            message,
            textAlign: TextAlign.center,
            style: TextStyle(color: Colors.white.withValues(alpha: .55)),
          ),
        ],
      ),
    ),
  );
}

Color severityColor(String severity) => switch (severity.toUpperCase()) {
  'CRITICAL' => const Color(0xFFFB7185),
  'ERROR' => const Color(0xFFF87171),
  'WARN' => const Color(0xFFFBBF24),
  'DEBUG' => const Color(0xFFA78BFA),
  _ => xactBlue,
};

Color statusColor(String status) {
  final value = status.toLowerCase();
  if (value.contains('bad') ||
      value.contains('error') ||
      value.contains('alarm')) {
    return const Color(0xFFF87171);
  }
  if (value.contains('warn') || value.contains('stale')) {
    return const Color(0xFFFBBF24);
  }
  return xactTeal;
}

String formatTimestamp(DateTime time) {
  final now = DateTime.now();
  final difference = now.difference(time);
  if (difference.inSeconds < 60) return 'now';
  if (difference.inMinutes < 60) return '${difference.inMinutes}m ago';
  if (difference.inHours < 24) return '${difference.inHours}h ago';
  return '${time.year}-${_two(time.month)}-${_two(time.day)} ${_two(time.hour)}:${_two(time.minute)}';
}

String _two(int value) => value.toString().padLeft(2, '0');

void showMessage(BuildContext context, String message) {
  ScaffoldMessenger.of(context)
    ..hideCurrentSnackBar()
    ..showSnackBar(SnackBar(content: Text(message)));
}
