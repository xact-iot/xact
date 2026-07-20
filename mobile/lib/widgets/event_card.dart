import 'package:flutter/material.dart';

import '../models/models.dart';
import 'common.dart';

class EventCard extends StatelessWidget {
  const EventCard({super.key, required this.event, this.onTap});
  final EventEntry event;
  final VoidCallback? onTap;

  @override
  Widget build(BuildContext context) {
    final color = severityColor(event.severity);
    return Card(
      child: InkWell(
        borderRadius: BorderRadius.circular(18),
        onTap: onTap,
        child: Padding(
          padding: const EdgeInsets.all(15),
          child: Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Container(
                width: 9,
                height: 9,
                margin: const EdgeInsets.only(top: 6),
                decoration: BoxDecoration(
                  shape: BoxShape.circle,
                  color: color,
                  boxShadow: [
                    BoxShadow(
                      color: color.withValues(alpha: .3),
                      blurRadius: 8,
                    ),
                  ],
                ),
              ),
              const SizedBox(width: 13),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Row(
                      children: [
                        Text(
                          event.severity,
                          style: TextStyle(
                            color: color,
                            fontSize: 11,
                            fontWeight: FontWeight.w800,
                            letterSpacing: .7,
                          ),
                        ),
                        const Spacer(),
                        Text(
                          formatTimestamp(event.timestamp),
                          style: TextStyle(
                            fontSize: 11,
                            color: Colors.white.withValues(alpha: .4),
                          ),
                        ),
                      ],
                    ),
                    const SizedBox(height: 6),
                    Text(
                      event.message,
                      style: const TextStyle(
                        fontWeight: FontWeight.w600,
                        height: 1.35,
                      ),
                    ),
                    if (event.device.isNotEmpty ||
                        event.userName.isNotEmpty) ...[
                      const SizedBox(height: 9),
                      Wrap(
                        spacing: 12,
                        runSpacing: 5,
                        children: [
                          if (event.device.isNotEmpty)
                            _Meta(
                              icon: Icons.memory_rounded,
                              value: event.device,
                            ),
                          if (event.userName.isNotEmpty)
                            _Meta(
                              icon: Icons.person_outline,
                              value: event.userName,
                            ),
                        ],
                      ),
                    ],
                  ],
                ),
              ),
              if (onTap != null)
                const Padding(
                  padding: EdgeInsets.only(left: 6, top: 24),
                  child: Icon(
                    Icons.chevron_right_rounded,
                    color: Colors.white30,
                  ),
                ),
            ],
          ),
        ),
      ),
    );
  }
}

class _Meta extends StatelessWidget {
  const _Meta({required this.icon, required this.value});
  final IconData icon;
  final String value;

  @override
  Widget build(BuildContext context) => Row(
    mainAxisSize: MainAxisSize.min,
    children: [
      Icon(icon, size: 14, color: Colors.white38),
      const SizedBox(width: 5),
      Text(
        value,
        style: TextStyle(
          fontSize: 12,
          color: Colors.white.withValues(alpha: .48),
        ),
      ),
    ],
  );
}
