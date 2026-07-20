import 'package:flutter/material.dart';
import 'package:open_filex/open_filex.dart';

import '../models/models.dart';
import '../services/api_client.dart';
import '../theme.dart';
import '../widgets/common.dart';

class ReportsScreen extends StatefulWidget {
  const ReportsScreen({super.key, required this.api});
  final XactApiClient api;

  @override
  State<ReportsScreen> createState() => _ReportsScreenState();
}

class _ReportsScreenState extends State<ReportsScreen> {
  List<ReportInfo> _reports = const [];
  bool _loading = true;
  String? _error;
  String? _downloading;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final reports = await widget.api.reports();
      if (mounted) setState(() => _reports = reports);
    } catch (error) {
      if (mounted) setState(() => _error = '$error');
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  Future<void> _open(ReportInfo report) async {
    setState(() => _downloading = report.id);
    try {
      final file = await widget.api.downloadReport(report);
      final result = await OpenFilex.open(file.path, type: 'application/pdf');
      if (result.type != ResultType.done && mounted) {
        showMessage(context, result.message);
      }
    } catch (error) {
      if (mounted) showMessage(context, 'Could not open report: $error');
    } finally {
      if (mounted) setState(() => _downloading = null);
    }
  }

  @override
  Widget build(BuildContext context) {
    if (_loading && _reports.isEmpty) {
      return const LoadingView(label: 'Loading reports');
    }
    if (_error != null && _reports.isEmpty) {
      return ErrorView(message: _error!, onRetry: _load);
    }
    if (_reports.isEmpty) {
      return const EmptyView(
        icon: Icons.picture_as_pdf_outlined,
        title: 'No reports available',
        message: 'There are no report templates available to your account.',
      );
    }

    return RefreshIndicator(
      onRefresh: _load,
      child: ListView.separated(
        physics: const AlwaysScrollableScrollPhysics(),
        padding: const EdgeInsets.fromLTRB(16, 10, 16, 24),
        itemCount: _reports.length,
        separatorBuilder: (_, _) => const SizedBox(height: 10),
        itemBuilder: (context, index) {
          final report = _reports[index];
          final busy = _downloading == report.id;
          return Card(
            child: InkWell(
              onTap: busy ? null : () => _open(report),
              borderRadius: BorderRadius.circular(18),
              child: Padding(
                padding: const EdgeInsets.all(16),
                child: Row(
                  children: [
                    Container(
                      width: 46,
                      height: 46,
                      decoration: BoxDecoration(
                        color: const Color(0xFFF87171).withValues(alpha: .1),
                        borderRadius: BorderRadius.circular(14),
                      ),
                      child: const Icon(
                        Icons.picture_as_pdf_rounded,
                        color: Color(0xFFF87171),
                      ),
                    ),
                    const SizedBox(width: 14),
                    Expanded(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text(
                            report.name,
                            style: const TextStyle(fontWeight: FontWeight.w700),
                          ),
                          if (report.description.isNotEmpty) ...[
                            const SizedBox(height: 5),
                            Text(
                              report.description,
                              maxLines: 2,
                              overflow: TextOverflow.ellipsis,
                              style: TextStyle(
                                fontSize: 12,
                                color: Colors.white.withValues(alpha: .48),
                              ),
                            ),
                          ],
                        ],
                      ),
                    ),
                    const SizedBox(width: 10),
                    busy
                        ? const SizedBox.square(
                            dimension: 22,
                            child: CircularProgressIndicator(strokeWidth: 2),
                          )
                        : const Icon(Icons.download_rounded, color: xactTeal),
                  ],
                ),
              ),
            ),
          );
        },
      ),
    );
  }
}
