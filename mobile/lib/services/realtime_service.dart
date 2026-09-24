import 'dart:async';
import 'dart:convert';

import 'package:web_socket_channel/web_socket_channel.dart';

import '../models/models.dart';
import 'api_client.dart';
import 'nats_parser.dart';

class TagUpdate {
  const TagUpdate({
    required this.path,
    required this.value,
    this.status = '',
    this.timestamp,
  });
  final String path;
  final dynamic value;
  final String status;
  final DateTime? timestamp;
}

class MobileNotification {
  const MobileNotification({
    required this.title,
    required this.body,
    this.device = '',
  });
  final String title;
  final String body;
  final String device;
}

class RealtimeService {
  RealtimeService(this.api, {WebSocketChannel Function(Uri)? connectChannel})
    : _connectChannel =
          connectChannel ??
          ((uri) => WebSocketChannel.connect(uri, protocols: const ['nats']));
  final XactApiClient api;
  final WebSocketChannel Function(Uri) _connectChannel;

  final _updates = StreamController<TagUpdate>.broadcast();
  final _notifications = StreamController<MobileNotification>.broadcast();
  WebSocketChannel? _channel;
  StreamSubscription<dynamic>? _subscription;
  late final _parser = NatsParser(
    onMessage: _handleMessage,
    onControl: (line) {
      if (line == 'PING') _send('PONG\r\n');
      if (line == 'PONG') _connected = true;
    },
  );
  String _org = '';
  String _userId = '';
  bool _connected = false;
  bool _disposed = false;
  int _generation = 0;

  Stream<TagUpdate> get updates => _updates.stream;
  Stream<MobileNotification> get notifications => _notifications.stream;
  bool get connected => _connected;

  Future<void> connect(XactUser user) async {
    if (_disposed) return;
    final generation = ++_generation;
    final sessionGeneration = api.generation;
    await _closeConnection();
    bool current() =>
        !_disposed &&
        generation == _generation &&
        sessionGeneration == api.generation;
    if (!current()) return;
    _org = user.tenantId;
    _userId = user.id;
    try {
      if ([
        user.tenantId,
        user.id,
      ].any((v) => v.isEmpty || RegExp(r'[\s.*>/\\]').hasMatch(v))) {
        return;
      }
      final config = await api.natsConfig();
      if (!current()) return;
      final path = '${config['natsWsPath'] ?? ''}';
      var url = '${config['natsWsUrl'] ?? ''}';
      if (url.isEmpty) {
        final base = Uri.parse(api.serverUrl);
        url = base.replace(scheme: 'wss', path: path).toString();
      }
      final uri = Uri.parse(url);
      XactApiClient.requireSecureUri(uri, scheme: 'wss');
      final channel = _connectChannel(uri);
      _channel = channel;
      await channel.ready.timeout(const Duration(seconds: 12));
      if (!current()) {
        await channel.sink.close();
        return;
      }
      _subscription = channel.stream.listen(
        _receive,
        onDone: () {
          if (current()) unawaited(disconnect());
        },
        onError: (_) {
          if (current()) unawaited(disconnect());
        },
        cancelOnError: false,
      );
      final connect = jsonEncode({
        'verbose': false,
        'pedantic': false,
        'tls_required': true,
        'name': 'xact-mobile',
        'lang': 'dart',
        'version': '1.0',
        'protocol': 1,
        'user': '${config['username'] ?? ''}',
        'pass': '${config['password'] ?? ''}',
      });
      _send('CONNECT $connect\r\n');
      _send('SUB xact.internal.bcast.tagvalue.$_org.> 1\r\nPING\r\n');
      _send('SUB xact.internal.bcast.mobile.$_org.${user.id} 2\r\n');
    } catch (_) {
      if (current()) await disconnect();
    }
  }

  void _send(String value) => _channel?.sink.add(value);

  void _receive(dynamic frame) {
    try {
      if (frame is String && frame.length <= NatsParser.maxBuffer) {
        _parser.add(utf8.encode(frame));
      } else if (frame is List<int>) {
        _parser.add(frame);
      } else {
        throw const FormatException('Invalid NATS frame');
      }
    } catch (_) {
      unawaited(disconnect());
    }
  }

  void _handleMessage(String subject, List<int> payload) {
    const prefix = 'xact.internal.bcast.tagvalue.';
    final orgPrefix = '$prefix$_org.';
    final mobileSubject = 'xact.internal.bcast.mobile.$_org.$_userId';
    if (subject == mobileSubject) {
      try {
        final data = jsonDecode(utf8.decode(payload)) as Map<String, dynamic>;
        _notifications.add(
          MobileNotification(
            title: '${data['title'] ?? 'XACT notification'}',
            body: '${data['body'] ?? ''}',
            device: '${data['device'] ?? ''}',
          ),
        );
      } catch (_) {}
      return;
    }
    if (!subject.startsWith(orgPrefix)) return;
    try {
      final decoded = jsonDecode(utf8.decode(payload)) as Map<String, dynamic>;
      if (decoded.isEmpty) return;
      final data = decoded.values.first as Map<String, dynamic>;
      final millis = (data['timestamp'] as num?)?.toInt();
      _updates.add(
        TagUpdate(
          path: '$_org.${subject.substring(orgPrefix.length)}',
          value: data['value'],
          status: '${data['status'] ?? ''}',
          timestamp: millis == null
              ? null
              : DateTime.fromMillisecondsSinceEpoch(millis).toLocal(),
        ),
      );
    } catch (_) {}
  }

  Future<void> disconnect() async {
    _generation++;
    await _closeConnection();
  }

  Future<void> _closeConnection() async {
    _connected = false;
    final subscription = _subscription;
    final channel = _channel;
    _subscription = null;
    _channel = null;
    _parser.clear();
    await subscription?.cancel();
    try {
      await channel?.sink.close().timeout(const Duration(seconds: 2));
    } catch (_) {}
  }

  Future<void> dispose() async {
    if (_disposed) return;
    _disposed = true;
    await disconnect();
    await _updates.close();
    await _notifications.close();
  }
}
