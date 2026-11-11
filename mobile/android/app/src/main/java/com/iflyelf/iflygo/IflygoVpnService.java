package com.iflyelf.iflygo;

import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.PendingIntent;
import android.content.Intent;
import android.net.VpnService;
import android.os.Build;
import android.os.ParcelFileDescriptor;
import android.util.Log;

import org.json.JSONArray;
import org.json.JSONObject;

import com.iflyelf.iflygo.core.Client;

public class IflygoVpnService extends VpnService {
    private static final String TAG = "IflygoVpnService";
    private static final String CHANNEL = "iflygo-vpn";
    private volatile boolean running = false;

    @Override
    public int onStartCommand(Intent intent, int flags, int startId) {
        startForeground(1, notification());
        if (running) {
            return START_STICKY;
        }
        IflygoApp app = (IflygoApp) getApplication();
        Client client = app.client();
        String raw;
        try {
            raw = client.vpnParams();
        } catch (Exception e) {
            Log.e(TAG, "vpnParams failed", e);
            stopSelf();
            return START_NOT_STICKY;
        }

        ParcelFileDescriptor pfd = null;
        try {
            JSONObject o = new JSONObject(raw);
            Builder b = new Builder();
            b.setSession("iFlyGo");
            b.setMtu(o.optInt("mtu", 1280));

            String v4 = o.optString("ipv4", "");
            int p4 = o.optInt("ipv4_prefix", 0);
            if (!v4.isEmpty() && p4 > 0) {
                b.addAddress(v4, p4);
            }
            String v6 = o.optString("ipv6", "");
            int p6 = o.optInt("ipv6_prefix", 0);
            if (!v6.isEmpty() && p6 > 0) {
                b.addAddress(v6, p6);
            }

            JSONArray routes = o.optJSONArray("routes");
            if (routes != null) {
                for (int i = 0; i < routes.length(); i++) {
                    String cidr = routes.optString(i, "");
                    int slash = cidr.indexOf('/');
                    if (slash <= 0) {
                        continue;
                    }
                    try {
                        b.addRoute(cidr.substring(0, slash), Integer.parseInt(cidr.substring(slash + 1)));
                    } catch (Exception e) {
                        Log.w(TAG, "skip route " + cidr, e);
                    }
                }
            }

            JSONArray dns = o.optJSONArray("dns");
            if (dns != null) {
                for (int i = 0; i < dns.length(); i++) {
                    try {
                        b.addDnsServer(dns.getString(i));
                    } catch (Exception e) {
                        Log.w(TAG, "skip dns", e);
                    }
                }
            }

            // Nebula 的 UDP 由本应用发出；排除本应用可避免 VPN 回环。
            try {
                b.addDisallowedApplication(getPackageName());
            } catch (Exception ignored) {
            }
            b.setBlocking(false);
            pfd = b.establish();
        } catch (Exception e) {
            Log.e(TAG, "establish failed", e);
            if (pfd != null) {
                try { pfd.close(); } catch (Exception ignored) {}
            }
            stopSelf();
            return START_NOT_STICKY;
        }
        if (pfd == null) {
            Log.e(TAG, "establish returned null");
            stopSelf();
            return START_NOT_STICKY;
        }

        final int fd = pfd.detachFd();
        pfd = null;
        running = true;
        new Thread(() -> {
            try {
                client.start(fd);
            } catch (Exception e) {
                Log.e(TAG, "start failed", e);
                running = false;
                stopSelf();
            }
        }, "iflygo-start").start();
        return START_STICKY;
    }

    private void stopTunnel() {
        if (!running) {
            return;
        }
        running = false;
        try {
            ((IflygoApp) getApplication()).client().stop();
        } catch (Exception e) {
            Log.w(TAG, "stop failed", e);
        }
    }

    @Override
    public void onRevoke() {
        stopTunnel();
        super.onRevoke();
    }

    @Override
    public void onDestroy() {
        stopTunnel();
        stopForeground(true);
        super.onDestroy();
    }

    private Notification notification() {
        if (Build.VERSION.SDK_INT >= 26) {
            NotificationManager nm = (NotificationManager) getSystemService(NOTIFICATION_SERVICE);
            NotificationChannel ch = new NotificationChannel(CHANNEL, "iFlyGo VPN", NotificationManager.IMPORTANCE_LOW);
            nm.createNotificationChannel(ch);
        }
        Intent intent = new Intent(this, MainActivity.class);
        PendingIntent pi = PendingIntent.getActivity(this, 0, intent,
                Build.VERSION.SDK_INT >= 23 ? PendingIntent.FLAG_IMMUTABLE : 0);
        Notification.Builder b = Build.VERSION.SDK_INT >= 26
                ? new Notification.Builder(this, CHANNEL)
                : new Notification.Builder(this);
        return b.setContentTitle("iFlyGo 隧道")
                .setContentText("正在运行")
                .setSmallIcon(android.R.drawable.ic_menu_manage)
                .setContentIntent(pi)
                .setOngoing(true)
                .build();
    }
}
