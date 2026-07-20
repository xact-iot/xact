import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';

import 'package:web_socket_channel/web_socket_channel.dart';

import '../models/models.dart';
import 'api_client.dart';

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
  RealtimeService(this.api);
  final XactApiClient api;

  final _updates = StreamController<TagUpdate>.broadcast();
  final _notifications = StreamController<MobileNotification>.broadcast();
  WebSocketChannel? _channel;
  StreamSubscription<dynamic>? _subscription;
  final List<int> _buffer = [];
  String _org = '';
  bool _connected = false;

  Stream<TagUpdate> get updates => _updates.stream;
  Stream<MobileNotification> get notifications => _notifications.stream;
  bool get connected => _connected;

  Future<void> connect(XactUser user) async {
    await disconnect();
    _org = user.tenantId;
    try {
      final config = await api.natsConfig();
      final path = '${config['natsWsPath'] ?? ''}';
      var url = '${config['natsWsUrl'] ?? ''}';
      if (url.isEmpty) {
        final base = Uri.parse(api.serverUrl);
        url = base
            .replace(
              scheme: base.scheme == 'https' ? 'wss' : 'ws',
              path: path,
              query: null,
              fragment: null,
            )
            .toString();
      }
      final channel = WebSocketChannel.connect(
        Uri.parse(url),
        protocols: const ['nats'],
      );
      await channel.ready.timeout(const Duration(seconds: 12));
      _channel = channel;
      _subscription = channel.stream.listen(
        _receive,
        onDone: () => _connected = false,
        onError: (_) => _connected = false,
        cancelOnError: false,
      );
      final connect = jsonEncode({
        'verbose': false,
        'pedantic': false,
        'tls_required': url.startsWith('wss:'),
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
      _connected = true;
    } catch (_) {
      await disconnect();
    }
  }

  void _send(String value) => _channel?.sink.add(value);

  void _receive(dynamic frame) {
    if (frame is String) {
      _buffer.addAll(utf8.encode(frame));
    } else if (frame is Uint8List) {
      _buffer.addAll(frame);
    } else if (frame is List<int>) {
      _buffer.addAll(frame);
    }
    _parse();
  }

  void _parse() {
    while (true) {
      final lineEnd = _indexOfCrlf(_buffer);
      if (lineEnd < 0) return;
      final line = utf8.decode(
        _buffer.sublist(0, lineEnd),
        allowMalformed: true,
      );
      if (line.startsWith('MSG ')) {
        final parts = line.split(' ');
        if (parts.length < 4) {
          _buffer.removeRange(0, lineEnd + 2);
          continue;
        }
        final size = int.tryParse(parts.last);
        if (size == null || _buffer.length < lineEnd + 2 + size + 2) return;
        final payloadStart = lineEnd + 2;
        final payload = _buffer.sublist(payloadStart, payloadStart + size);
        _buffer.removeRange(0, payloadStart + size + 2);
        _handleMessage(parts[1], payload);
        continue;
      }
      _buffer.removeRange(0, lineEnd + 2);
      if (line == 'PING') _send('PONG\r\n');
    }
  }

  int _indexOfCrlf(List<int> bytes) {
    for (var i = 0; i < bytes.length - 1; i++) {
      if (bytes[i] == 13 && bytes[i + 1] == 10) return i;
    }
    return -1;
  }

  void _handleMessage(String subject, List<int> payload) {
    const prefix = 'xact.internal.bcast.tagvalue.';
    final orgPrefix = '$prefix$_org.';
    final mobileSubject = 'xact.internal.bcast.mobile.$_org.';
    if (subject.startsWith(mobileSubject)) {
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
    _connected = false;
    await _subscription?.cancel();
    _subscription = null;
    await _channel?.sink.close();
    _channel = null;
    _buffer.clear();
  }

  Future<void> dispose() async {
    await disconnect();
    await _updates.close();
    await _notifications.close();
  }
}
