import 'package:flutter/material.dart';
import 'package:flutter_inappwebview/flutter_inappwebview.dart';

import '../l10n/generated/app_localizations.dart';

/// Drives the Cloudflare Access browser flow inside an in-app webview
/// and captures the resulting `CF_Authorization` session cookie.
///
/// The flow:
///   1. Open the protected probe URL `<baseUrl>/api/health`.
///   2. Cloudflare Access intercepts and redirects to its login UI, then
///      to Google, then back to the probe URL.
///   3. The relay returns 401 (no LAN bearer attached), but by the time
///      we observe the response the cookie is already set on the host.
///   4. Read the cookie jar for the host. When `CF_Authorization` is
///      present, pop the screen with the value of that cookie.
///
/// Returns the cookie string formatted as `CF_Authorization=<value>` (so
/// it can be dropped straight into a `Cookie` header), or `null` if the
/// user backed out before completion.
class CloudflareSsoScreen extends StatefulWidget {
  const CloudflareSsoScreen({super.key, required this.baseUrl});

  final String baseUrl;

  @override
  State<CloudflareSsoScreen> createState() => _CloudflareSsoScreenState();
}

class _CloudflareSsoScreenState extends State<CloudflareSsoScreen> {
  bool _captured = false;
  bool _loading = true;
  InAppWebViewController? _controller;

  String get _probeUrl => '${widget.baseUrl}/api/health';

  Future<void> _maybeCapture(InAppWebViewController _) async {
    if (_captured) return;
    final base = Uri.parse(widget.baseUrl);
    try {
      final cookies = await CookieManager.instance().getCookies(
        url: WebUri(widget.baseUrl),
      );
      String? cf;
      for (final c in cookies) {
        if (c.name == 'CF_Authorization' && (c.value?.isNotEmpty ?? false)) {
          cf = c.value as String?;
          break;
        }
      }
      if (cf == null || cf.isEmpty) return;
      _captured = true;
      if (!mounted) return;
      Navigator.of(context).pop('CF_Authorization=$cf');
      // Reference base so the local is not flagged unused.
      assert(base.host.isNotEmpty);
    } catch (_) {
      // Swallow cookie-read errors; the user can retry by reloading.
    }
  }

  // Wipes the in-app webview's cookie jar (Cloudflare Access + Google) and
  // reloads the probe URL. Forces Google to show its account chooser instead
  // of silently reusing the previously-signed-in account.
  Future<void> _switchAccount() async {
    final controller = _controller;
    if (controller == null) return;
    try {
      await CookieManager.instance().deleteAllCookies();
    } catch (_) {
      // Best-effort: even if cookie deletion fails, try the reload anyway.
    }
    if (!mounted) return;
    setState(() => _loading = true);
    await controller.loadUrl(
      urlRequest: URLRequest(url: WebUri(_probeUrl)),
    );
  }

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context)!;
    return Scaffold(
      appBar: AppBar(
        title: Text(t.ssoLoginTitle),
        actions: [
          IconButton(
            icon: const Icon(Icons.switch_account),
            tooltip: t.ssoSwitchAccountTooltip,
            onPressed: _controller == null ? null : _switchAccount,
          ),
          TextButton(
            onPressed: () => Navigator.of(context).pop(),
            child: Text(
              t.cancel,
              style: TextStyle(color: Theme.of(context).colorScheme.onPrimary),
            ),
          ),
        ],
      ),
      body: Stack(
        children: [
          InAppWebView(
            initialUrlRequest: URLRequest(url: WebUri(_probeUrl)),
            initialSettings: InAppWebViewSettings(
              clearCache: false,
              thirdPartyCookiesEnabled: true,
              javaScriptEnabled: true,
              userAgent:
                  'Mozilla/5.0 (Linux; Android) httpssh-mobile/0.1 InAppWebView',
            ),
            onWebViewCreated: (controller) {
              setState(() => _controller = controller);
            },
            onLoadStart: (_, __) => setState(() => _loading = true),
            onLoadStop: (controller, url) async {
              setState(() => _loading = false);
              await _maybeCapture(controller);
            },
            onReceivedHttpError: (controller, _, __) async {
              // The relay returns 401 (no bearer) on the probe URL once
              // Cloudflare lets us through. That means the cookie is set;
              // try to capture even though the page itself returned an
              // error.
              await _maybeCapture(controller);
            },
          ),
          if (_loading) const LinearProgressIndicator(minHeight: 2),
        ],
      ),
    );
  }
}
