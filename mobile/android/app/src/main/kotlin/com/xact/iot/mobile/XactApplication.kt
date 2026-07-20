package com.xact.iot.mobile

import android.app.Application
import com.google.firebase.FirebaseApp
import com.google.firebase.FirebaseOptions

class XactApplication : Application() {
    override fun onCreate() {
        super.onCreate()
        val prefs = getSharedPreferences(PREFS_NAME, MODE_PRIVATE)
        val apiKey = prefs.getString(API_KEY, null)
        val appId = prefs.getString(APP_ID, null)
        val projectId = prefs.getString(PROJECT_ID, null)
        val senderId = prefs.getString(SENDER_ID, null)
        if (!apiKey.isNullOrBlank() && !appId.isNullOrBlank() &&
            !projectId.isNullOrBlank() && !senderId.isNullOrBlank() &&
            FirebaseApp.getApps(this).isEmpty()
        ) {
            val options = FirebaseOptions.Builder()
                .setApiKey(apiKey)
                .setApplicationId(appId)
                .setProjectId(projectId)
                .setGcmSenderId(senderId)
                .build()
            FirebaseApp.initializeApp(this, options)
        }
    }

    companion object {
        const val PREFS_NAME = "xact_firebase"
        const val API_KEY = "api_key"
        const val APP_ID = "app_id"
        const val PROJECT_ID = "project_id"
        const val SENDER_ID = "sender_id"
    }
}
