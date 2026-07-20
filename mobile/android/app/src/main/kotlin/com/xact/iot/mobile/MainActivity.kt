package com.xact.iot.mobile

import android.app.AlarmManager
import android.app.PendingIntent
import android.content.Context
import android.os.Process
import android.os.SystemClock
import io.flutter.embedding.android.FlutterActivity
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.plugin.common.MethodChannel

class MainActivity : FlutterActivity() {
    override fun configureFlutterEngine(flutterEngine: FlutterEngine) {
        super.configureFlutterEngine(flutterEngine)
        MethodChannel(
            flutterEngine.dartExecutor.binaryMessenger,
            "com.xact.iot.mobile/firebase_bootstrap",
        ).setMethodCallHandler { call, result ->
            when (call.method) {
                "storeConfig" -> {
                    val values = call.arguments as? Map<*, *>
                    val apiKey = values?.get("apiKey") as? String
                    val appId = values?.get("appId") as? String
                    val projectId = values?.get("projectId") as? String
                    val senderId = values?.get("messagingSenderId") as? String
                    if (apiKey.isNullOrBlank() || appId.isNullOrBlank() ||
                        projectId.isNullOrBlank() || senderId.isNullOrBlank()
                    ) {
                        result.error("invalid_config", "Firebase configuration is incomplete", null)
                        return@setMethodCallHandler
                    }
                    getSharedPreferences(XactApplication.PREFS_NAME, MODE_PRIVATE)
                        .edit()
                        .putString(XactApplication.API_KEY, apiKey)
                        .putString(XactApplication.APP_ID, appId)
                        .putString(XactApplication.PROJECT_ID, projectId)
                        .putString(XactApplication.SENDER_ID, senderId)
                        .apply()
                    result.success(null)
                }
                "clearConfig" -> {
                    getSharedPreferences(XactApplication.PREFS_NAME, MODE_PRIVATE)
                        .edit().clear().apply()
                    result.success(null)
                }
                "restartApp" -> {
                    val intent = packageManager.getLaunchIntentForPackage(packageName)
                    if (intent == null) {
                        result.error("restart_failed", "Unable to create launch intent", null)
                        return@setMethodCallHandler
                    }
                    intent.addFlags(android.content.Intent.FLAG_ACTIVITY_NEW_TASK or android.content.Intent.FLAG_ACTIVITY_CLEAR_TOP)
                    val pending = PendingIntent.getActivity(
                        this,
                        9137,
                        intent,
                        PendingIntent.FLAG_CANCEL_CURRENT or PendingIntent.FLAG_IMMUTABLE,
                    )
                    val alarm = getSystemService(Context.ALARM_SERVICE) as AlarmManager
                    alarm.set(
                        AlarmManager.ELAPSED_REALTIME,
                        SystemClock.elapsedRealtime() + 300,
                        pending,
                    )
                    result.success(null)
                    finishAffinity()
                    Process.killProcess(Process.myPid())
                }
                else -> result.notImplemented()
            }
        }
    }
}
