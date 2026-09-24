import 'dart:convert';

/// Bounded NATS framing. Protocol violations require dropping the connection.
class NatsParser {
  NatsParser({required this.onMessage, required this.onControl});
  static const maxHeader = 8192;
  static const maxPayload = 1024 * 1024;
  static const maxBuffer = maxPayload + maxHeader + 4;
  final void Function(String subject, List<int> payload) onMessage;
  final void Function(String line) onControl;
  final List<int> _buffer = [];

  void clear() => _buffer.clear();

  void add(List<int> bytes) {
    if (_buffer.length + bytes.length > maxBuffer) {
      throw const FormatException('NATS receive buffer limit exceeded');
    }
    _buffer.addAll(bytes);
    var offset = 0;
    while (offset < _buffer.length) {
      var end = -1;
      for (
        var i = offset;
        i < _buffer.length - 1 && i - offset <= maxHeader;
        i++
      ) {
        if (_buffer[i] == 13 && _buffer[i + 1] == 10) {
          end = i;
          break;
        }
      }
      if (end < 0) {
        final pending = _buffer.length - offset;
        if (pending > maxHeader + 1 ||
            (pending == maxHeader + 1 && _buffer.last != 13)) {
          throw const FormatException('NATS header limit exceeded');
        }
        break;
      }
      if (end - offset > maxHeader) {
        throw const FormatException('NATS header limit exceeded');
      }
      final line = utf8.decode(_buffer.sublist(offset, end));
      if (line.startsWith('MSG ')) {
        final parts = line.split(RegExp(r' +'));
        if ((parts.length != 4 && parts.length != 5) ||
            !RegExp(r'^\d{1,10}$').hasMatch(parts.last) ||
            !RegExp(r'^\d+$').hasMatch(parts[2]) ||
            parts[1].isEmpty) {
          throw const FormatException('Invalid NATS message header');
        }
        final size = int.parse(parts.last);
        if (size > maxPayload) {
          throw const FormatException('NATS payload limit exceeded');
        }
        final start = end + 2;
        if (_buffer.length < start + size + 2) break;
        if (_buffer[start + size] != 13 || _buffer[start + size + 1] != 10) {
          throw const FormatException('Invalid NATS payload terminator');
        }
        final payload = _buffer.sublist(start, start + size);
        offset = start + size + 2;
        onMessage(parts[1], payload);
      } else {
        offset = end + 2;
        if (line == 'PING' ||
            line == 'PONG' ||
            line == '+OK' ||
            line.startsWith('INFO ')) {
          onControl(line);
        } else {
          throw const FormatException('NATS protocol error');
        }
      }
    }
    // Compact once per WebSocket frame: removing each message's prefix would
    // make a large batch of tiny messages take quadratic copying work.
    if (offset > 0) _buffer.removeRange(0, offset);
  }
}
