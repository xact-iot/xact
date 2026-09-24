import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:webview_flutter/webview_flutter.dart';

import '../models/models.dart';
import '../services/api_client.dart';
import '../services/web_session.dart';
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
  late final int _sessionGeneration;

  @override
  void initState() {
    super.initState();
    _sessionGeneration = widget.api.generation;
    _selected = widget.initialDashboardId;
    _load();
  }

  Future<void> _load() async {
    if (mounted) {
      setState(() {
        _loading = true;
        _error = null;
      });
    }
    try {
      final responses = await Future.wait<dynamic>([
        widget.api.dashboards(),
        widget.api.mobileAppConfig(),
      ]);
      final dashboards = responses[0] as List<DashboardInfo>;
      final config = responses[1] as MobileAppConfig;
      if (_selected == null ||
          !dashboards.any((item) => item.id == _selected)) {
        final configured = config.defaultDashboardName.trim().toLowerCase();
        _selected = configured.isEmpty
            ? dashboards.firstOrNull?.id
            : dashboards
                      .where((item) => item.name.toLowerCase() == configured)
                      .toList()
                      .firstOrNull
                      ?.id ??
                  dashboards.firstOrNull?.id;
      }
      if (!_current) return;
      setState(() => _dashboards = dashboards);
      if (dashboards.isNotEmpty) await _createWebView();
    } catch (error) {
      if (mounted) setState(() => _error = '$error');
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  bool get _current => mounted && widget.api.generation == _sessionGeneration;

  Future<void> _createWebView() async {
    await WebSession.clear();
    if (!_current) return;
    _authInjected = false;
    final bootstrap = widget.api.dashboardBootstrapUri;
    final dashboard = Uri.parse(widget.api.dashboardUrl(_selected));
    late final WebViewController controller;
    controller = WebViewController()
      ..setJavaScriptMode(JavaScriptMode.unrestricted)
      ..setBackgroundColor(const Color(0xFF081521))
      ..setNavigationDelegate(
        NavigationDelegate(
          onNavigationRequest: (request) {
            if (request.url == 'about:blank') {
              return NavigationDecision.navigate;
            }
            final uri = Uri.tryParse(request.url);
            final allowed =
                _current &&
                uri != null &&
                uri.userInfo.isEmpty &&
                uri.scheme == 'https' &&
                uri.origin == bootstrap.origin &&
                (uri == bootstrap ||
                    (uri.path == dashboard.path &&
                        uri.query == dashboard.query));
            return allowed
                ? NavigationDecision.navigate
                : NavigationDecision.prevent;
          },
          onProgress: (value) {
            if (mounted) setState(() => _progress = value);
          },
          onPageFinished: (url) async {
            if (!_current || _authInjected || Uri.tryParse(url) != bootstrap) {
              return;
            }
            _authInjected = true;
            final token = jsonEncode(widget.session.token);
            final user = jsonEncode(jsonEncode(widget.session.user.toJson()));
            final target = jsonEncode(widget.api.dashboardUrl(_selected));
            // Store the session and navigate in one JavaScript task so the
            // web app cannot begin another load between those operations.
            try {
              await controller.runJavaScript('''
if (location.href === ${jsonEncode(bootstrap.toString())}) {
  sessionStorage.setItem('xact_auth_token', $token);
  sessionStorage.setItem('xact_auth_user', $user);
  location.replace($target);
}
''');
            } catch (_) {
              if (_current) {
                setState(
                  () => _error = 'Could not open the dashboard securely.',
                );
              }
            }
          },
        ),
      );
    _web = controller;
    WebSession.register(controller);
    await controller.loadRequest(bootstrap);
  }

  Future<void> _select(int? id) async {
    if (!_current || id == null || id == _selected) return;
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
