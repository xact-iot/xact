import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:open_filex/open_filex.dart';
import 'package:package_info_plus/package_info_plus.dart';

import '../models/models.dart';
import '../services/notification_service.dart';
import '../services/session_controller.dart';
import '../theme.dart';
import '../widgets/common.dart';

class SettingsScreen extends StatefulWidget {
  const SettingsScreen({
    super.key,
    required this.controller,
    required this.notifications,
  });
  final SessionController controller;
  final NotificationService notifications;

  @override
  State<SettingsScreen> createState() => _SettingsScreenState();
}

class _SettingsScreenState extends State<SettingsScreen> {
  List<Organisation> _organisations = const [];
  bool _switching = false;
  bool _notificationBusy = false;
  bool _updateBusy = false;
  Map<String, dynamic>? _health;
  String _appVersion = '';

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    final package = await PackageInfo.fromPlatform();
    List<Organisation> organisations = const [];
    Map<String, dynamic>? health;
    try {
      final results = await Future.wait<dynamic>([
        widget.controller.api.organisations(),
        widget.controller.api.health(),
      ]);
      organisations = results[0] as List<Organisation>;
      health = results[1] as Map<String, dynamic>;
    } catch (_) {}
    if (mounted) {
      setState(() {
        _appVersion = '${package.version}+${package.buildNumber}';
        _organisations = organisations;
        _health = health;
      });
    }
  }

  Future<void> _switchOrg(String? org) async {
    if (org == null || org == widget.controller.session?.user.tenantId) return;
    setState(() => _switching = true);
    try {
      await widget.controller.switchOrganisation(org);
    } catch (error) {
      if (mounted) {
        showMessage(context, 'Could not switch organisation: $error');
      }
    } finally {
      if (mounted) setState(() => _switching = false);
    }
  }

  Future<void> _toggleNotifications(bool enabled) async {
    setState(() => _notificationBusy = true);
    try {
      if (enabled && !await widget.notifications.requestPermission()) {
        throw Exception('Notification permission was not granted.');
      }
      await widget.controller.setNotifications(enabled);
      if (enabled) {
        await widget.notifications.start();
      } else {
        widget.notifications.stop();
      }
    } catch (error) {
      if (mounted) {
        showMessage(context, 'Could not update notifications: $error');
      }
    } finally {
      if (mounted) setState(() => _notificationBusy = false);
    }
  }

  Future<void> _checkForUpdate() async {
    setState(() => _updateBusy = true);
    try {
      final package = await PackageInfo.fromPlatform();
      final release = await widget.controller.api.mobileRelease();
      if (!mounted) return;
      if (release == null) {
        showMessage(
          context,
          'This server does not publish a mobile release manifest.',
        );
      } else if (_compareVersions(release.version, package.version) <= 0) {
        showMessage(context, 'XACT Mobile is up to date.');
      } else {
        await _offerUpdate(release);
      }
    } catch (error) {
      if (mounted) showMessage(context, 'Update check failed: $error');
    } finally {
      if (mounted) setState(() => _updateBusy = false);
    }
  }

  Future<void> _offerUpdate(MobileRelease release) async {
    final install = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: Text('Version ${release.version} available'),
        content: Text(
          release.notes.isEmpty
              ? 'Download and install the update from your XACT server?'
              : release.notes,
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('Later'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, true),
            child: const Text('Download'),
          ),
        ],
      ),
    );
    if (install != true || !mounted) return;
    if (defaultTargetPlatform != TargetPlatform.android) {
      showMessage(
        context,
        'Self-hosted installation is currently available on Android.',
      );
      return;
    }
    final apk = await widget.controller.api.downloadApk(release);
    final result = await OpenFilex.open(
      apk.path,
      type: 'application/vnd.android.package-archive',
    );
    if (result.type != ResultType.done && mounted) {
      showMessage(context, result.message);
    }
  }

  int _compareVersions(String a, String b) {
    final left = a
        .split(RegExp(r'[+.-]'))
        .map((part) => int.tryParse(part) ?? 0)
        .toList();
    final right = b
        .split(RegExp(r'[+.-]'))
        .map((part) => int.tryParse(part) ?? 0)
        .toList();
    for (var i = 0; i < left.length || i < right.length; i++) {
      final l = i < left.length ? left[i] : 0;
      final r = i < right.length ? right[i] : 0;
      if (l != r) return l.compareTo(r);
    }
    return 0;
  }

  @override
  Widget build(BuildContext context) {
    final session = widget.controller.session!;
    final currentOrg = session.user.tenantId;
    return ListView(
      padding: const EdgeInsets.fromLTRB(16, 8, 16, 28),
      children: [
        Card(
          child: Padding(
            padding: const EdgeInsets.all(18),
            child: Row(
              children: [
                CircleAvatar(
                  radius: 26,
                  backgroundColor: xactTeal.withValues(alpha: .14),
                  child: Text(
                    session.user.username.isEmpty
                        ? '?'
                        : session.user.username[0].toUpperCase(),
                    style: const TextStyle(
                      color: xactTeal,
                      fontWeight: FontWeight.w800,
                      fontSize: 20,
                    ),
                  ),
                ),
                const SizedBox(width: 14),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        session.user.username,
                        style: const TextStyle(
                          fontWeight: FontWeight.w700,
                          fontSize: 16,
                        ),
                      ),
                      const SizedBox(height: 4),
                      Text(
                        session.user.roles.join(' • '),
                        maxLines: 2,
                        overflow: TextOverflow.ellipsis,
                        style: const TextStyle(
                          fontSize: 12,
                          color: Colors.white38,
                        ),
                      ),
                    ],
                  ),
                ),
                IconButton(
                  onPressed: widget.controller.logout,
                  tooltip: 'Sign out',
                  icon: const Icon(Icons.logout_rounded),
                ),
              ],
            ),
          ),
        ),
        const SizedBox(height: 22),
        const _Label('ORGANISATION'),
        const SizedBox(height: 9),
        DropdownButtonFormField<String>(
          initialValue: currentOrg,
          decoration: InputDecoration(
            prefixIcon: const Icon(Icons.apartment_rounded),
            suffixIcon: _switching
                ? const Padding(
                    padding: EdgeInsets.all(14),
                    child: CircularProgressIndicator(strokeWidth: 2),
                  )
                : null,
          ),
          items:
              (_organisations.isEmpty
                      ? [
                          Organisation(
                            name: currentOrg,
                            displayName: currentOrg,
                          ),
                        ]
                      : _organisations)
                  .map(
                    (org) => DropdownMenuItem(
                      value: org.name,
                      child: Text(org.displayName),
                    ),
                  )
                  .toList(),
          onChanged: _switching ? null : _switchOrg,
        ),
        const SizedBox(height: 22),
        const _Label('NOTIFICATIONS'),
        const SizedBox(height: 9),
        Card(
          child: SwitchListTile.adaptive(
            value: widget.controller.notificationsEnabled,
            onChanged: _notificationBusy ? null : _toggleNotifications,
            secondary: const Icon(
              Icons.notifications_active_outlined,
              color: xactTeal,
            ),
            title: const Text('Server notifications'),
            subtitle: const Text(
              'Display subscribed XACT events on this device',
            ),
          ),
        ),
        const SizedBox(height: 22),
        const _Label('SOFTWARE'),
        const SizedBox(height: 9),
        Card(
          child: Column(
            children: [
              ListTile(
                leading: const Icon(
                  Icons.phone_android_rounded,
                  color: xactTeal,
                ),
                title: const Text('XACT Mobile'),
                subtitle: Text(
                  _appVersion.isEmpty
                      ? 'Checking version…'
                      : 'Version $_appVersion',
                ),
              ),
              Divider(
                height: 1,
                indent: 16,
                endIndent: 16,
                color: Colors.white.withValues(alpha: .06),
              ),
              ListTile(
                leading: const Icon(Icons.dns_outlined, color: xactBlue),
                title: const Text('XACT Server'),
                subtitle: Text(
                  '${_health?['appVersion'] ?? session.serverUrl}',
                ),
              ),
              Divider(
                height: 1,
                indent: 16,
                endIndent: 16,
                color: Colors.white.withValues(alpha: .06),
              ),
              ListTile(
                onTap: _updateBusy ? null : _checkForUpdate,
                leading: const Icon(Icons.system_update_alt_rounded),
                title: const Text('Check for mobile update'),
                trailing: _updateBusy
                    ? const SizedBox.square(
                        dimension: 20,
                        child: CircularProgressIndicator(strokeWidth: 2),
                      )
                    : const Icon(Icons.chevron_right_rounded),
              ),
            ],
          ),
        ),
        const SizedBox(height: 18),
        Text(
          session.serverUrl,
          textAlign: TextAlign.center,
          style: const TextStyle(fontSize: 11, color: Colors.white30),
        ),
      ],
    );
  }
}

class _Label extends StatelessWidget {
  const _Label(this.value);
  final String value;

  @override
  Widget build(BuildContext context) => Text(
    value,
    style: const TextStyle(
      fontSize: 11,
      color: xactTeal,
      letterSpacing: 1.2,
      fontWeight: FontWeight.w800,
    ),
  );
}
