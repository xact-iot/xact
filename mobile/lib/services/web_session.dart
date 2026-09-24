import 'package:webview_flutter/webview_flutter.dart';

/// Owns browser cleanup independently of any dashboard widget's lifetime.
class WebSession {
  static final _controllers = <WebViewController>{};
  static Future<void>? _pending;

  static void register(WebViewController controller) =>
      _controllers.add(controller);

  static Future<void> clear() {
    final pending = _pending;
    if (pending != null) return pending;
    return _pending = _clear().whenComplete(() => _pending = null);
  }

  static Future<void> _clear() async {
    if (WebViewPlatform.instance == null) return;
    final controllers = _controllers.toList();
    _controllers.clear();
    Object? failure;
    StackTrace? failureStack;
    Future<void> attempt(Future<void> Function() action) async {
      try {
        await action();
      } catch (error, stack) {
        failure ??= error;
        failureStack ??= stack;
      }
    }

    for (final controller in controllers) {
      await attempt(
        () =>
            controller.runJavaScript('window.stop(); sessionStorage.clear();'),
      );
      await attempt(() => controller.loadRequest(Uri.parse('about:blank')));
    }
    final controller = controllers.firstOrNull ?? WebViewController();
    await attempt(controller.clearLocalStorage);
    await attempt(controller.clearCache);
    await attempt(() async {
      await WebViewCookieManager().clearCookies();
    });
    if (failure != null) {
      _controllers.addAll(controllers);
      Error.throwWithStackTrace(failure!, failureStack!);
    }
  }
}
