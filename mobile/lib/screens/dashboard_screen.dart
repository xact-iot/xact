import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:webview_flutter/webview_flutter.dart';

import '../models/models.dart';
import '../services/api_client.dart';
import '../widgets/common.dart';

class DashboardScreen extends StatefulWidget {
  const DashboardScreen({
    super.key,
    required this.api,
    required this.session,
    required this.initialDashboardId,
    required this.onDashboardChanged,
  });
  final XactApiClient api;
  final AuthSession session;
  final int? initialDashboardId;
  final ValueChanged<int?> onDashboardChanged;

  @override
  State<DashboardScreen> createState() => _DashboardScreenState();
}

class _DashboardScreenState extends State<DashboardScreen> {
  WebViewController? _web;
  List<DashboardInfo> _dashboards = const [];
  int? _selected;
  bool _loading = true;
  String? _error;
  bool _authInjected = false;
  int _progress = 0;

  @override
  void initState() {
    super.initState();
    _selected = widget.initialDashboardId;
    _load();
  }

  Future<void> _load() async {
    try {
      final dashboards = await widget.api.dashboards();
      if (_selected == null ||
          !dashboards.any((item) => item.id == _selected)) {
        _selected = dashboards.firstOrNull?.id;
      }
      if (mounted) setState(() => _dashboards = dashboards);
      if (dashboards.isNotEmpty) _createWebView();
    } catch (error) {
      if (mounted) setState(() => _error = '$error');
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  void _createWebView() {
    late final WebViewController controller;
    controller = WebViewController()
      ..setJavaScriptMode(JavaScriptMode.unrestricted)
      ..setBackgroundColor(const Color(0xFF081521))
      ..setNavigationDelegate(
        NavigationDelegate(
          onProgress: (value) {
            if (mounted) setState(() => _progress = value);
          },
          onPageFinished: (_) async {
            if (_authInjected) return;
            _authInjected = true;
            final user = jsonEncode(widget.session.user.toJson());
            final token = jsonEncode(widget.session.token);
            await controller.runJavaScript(
              "localStorage.setItem('xact_auth_token', $token);"
              "localStorage.setItem('xact_auth_user', ${jsonEncode(user)});",
            );
            await controller.loadRequest(
              Uri.parse(widget.api.dashboardUrl(_selected)),
            );
          },
        ),
      );
    _web = controller;
    controller.loadRequest(Uri.parse('${widget.api.serverUrl}/'));
  }

  Future<void> _select(int? id) async {
    if (id == null || id == _selected) return;
    setState(() => _selected = id);
    widget.onDashboardChanged(id);
    await _web?.loadRequest(Uri.parse(widget.api.dashboardUrl(id)));
  }

  @override
  Widget build(BuildContext context) {
    if (_loading) return const LoadingView(label: 'Loading dashboards');
    if (_error != null) return ErrorView(message: _error!, onRetry: _load);
    if (_dashboards.isEmpty) {
      return const EmptyView(
        icon: Icons.dashboard_outlined,
        title: 'No dashboards',
        message:
            'No dashboard is available for your current organisation and permissions.',
      );
    }

    return Column(
      children: [
        Padding(
          padding: const EdgeInsets.fromLTRB(16, 7, 16, 10),
          child: DropdownButtonFormField<int>(
            initialValue: _selected,
            decoration: const InputDecoration(
              labelText: 'Dashboard',
              prefixIcon: Icon(Icons.dashboard_customize_outlined),
              contentPadding: EdgeInsets.symmetric(
                horizontal: 14,
                vertical: 11,
              ),
            ),
            items: _dashboards
                .map(
                  (item) =>
                      DropdownMenuItem(value: item.id, child: Text(item.name)),
                )
                .toList(),
            onChanged: _select,
          ),
        ),
        if (_progress < 100)
          LinearProgressIndicator(value: _progress / 100, minHeight: 2),
        Expanded(
          child: _web == null
              ? const SizedBox()
              : WebViewWidget(controller: _web!),
        ),
      ],
    );
  }
}

extension _FirstDashboard<T> on List<T> {
  T? get firstOrNull => isEmpty ? null : first;
}
