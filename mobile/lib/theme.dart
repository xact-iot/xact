import 'package:flutter/material.dart';

const xactNavy = Color(0xFF081521);
const xactPanel = Color(0xFF102332);
const xactTeal = Color(0xFF2DD4BF);
const xactBlue = Color(0xFF38BDF8);

ThemeData buildXactTheme() {
  final scheme =
      ColorScheme.fromSeed(
        seedColor: xactTeal,
        brightness: Brightness.dark,
        surface: xactPanel,
      ).copyWith(
        primary: xactTeal,
        secondary: xactBlue,
        surface: xactPanel,
        error: const Color(0xFFF87171),
      );

  return ThemeData(
    useMaterial3: true,
    colorScheme: scheme,
    scaffoldBackgroundColor: xactNavy,
    fontFamily: 'sans-serif',
    appBarTheme: const AppBarTheme(
      backgroundColor: Colors.transparent,
      elevation: 0,
      centerTitle: false,
    ),
    cardTheme: CardThemeData(
      color: xactPanel,
      elevation: 0,
      margin: EdgeInsets.zero,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(18),
        side: BorderSide(color: Colors.white.withValues(alpha: .07)),
      ),
    ),
    inputDecorationTheme: InputDecorationTheme(
      filled: true,
      fillColor: Colors.white.withValues(alpha: .045),
      border: OutlineInputBorder(
        borderRadius: BorderRadius.circular(14),
        borderSide: BorderSide.none,
      ),
      enabledBorder: OutlineInputBorder(
        borderRadius: BorderRadius.circular(14),
        borderSide: BorderSide(color: Colors.white.withValues(alpha: .08)),
      ),
      focusedBorder: OutlineInputBorder(
        borderRadius: BorderRadius.circular(14),
        borderSide: const BorderSide(color: xactTeal),
      ),
    ),
    navigationBarTheme: NavigationBarThemeData(
      backgroundColor: const Color(0xFF0C1C29),
      indicatorColor: xactTeal.withValues(alpha: .18),
      height: 72,
    ),
  );
}
