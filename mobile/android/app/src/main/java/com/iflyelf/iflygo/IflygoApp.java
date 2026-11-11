package com.iflyelf.iflygo;

import android.app.Application;
import android.content.SharedPreferences;

import java.io.File;

import com.iflyelf.iflygo.core.mobile.Client;
public class IflygoApp extends Application {
    private static final String PREFS = "iflygo";
    private Client client;

    @Override
    public void onCreate() {
        super.onCreate();
        File dir = new File(getFilesDir(), "iflygo");
        //noinspection ResultOfMethodCallIgnored
        dir.mkdirs();
        SharedPreferences sp = getSharedPreferences(PREFS, MODE_PRIVATE);
        client = new Client(sp.getString("server", ""), dir.getAbsolutePath());
    }

    public Client client() {
        return client;
    }

    public String server() {
        return getSharedPreferences(PREFS, MODE_PRIVATE).getString("server", "");
    }

    public void saveServer(String server) {
        getSharedPreferences(PREFS, MODE_PRIVATE).edit().putString("server", server).apply();
    }
}
