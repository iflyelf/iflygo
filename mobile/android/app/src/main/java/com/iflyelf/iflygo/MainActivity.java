package com.iflyelf.iflygo;

import android.Manifest;
import android.app.Activity;
import android.content.Intent;
import android.content.pm.PackageManager;
import android.graphics.Color;
import android.graphics.Typeface;
import android.net.VpnService;
import android.os.Build;
import android.os.Bundle;
import android.os.Handler;
import android.os.Looper;
import android.text.InputType;
import android.view.Gravity;
import android.view.View;
import android.view.ViewGroup;
import android.widget.Button;
import android.widget.EditText;
import android.widget.LinearLayout;
import android.widget.ScrollView;
import android.widget.TextView;
import android.widget.Toast;

import com.iflyelf.iflygo.core.mobile.Client;

import org.json.JSONObject;

import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;

public class MainActivity extends Activity {
    private static final int REQ_VPN = 100;
    private static final int REQ_NOTIFY = 101;

    private final ExecutorService io = Executors.newSingleThreadExecutor();
    private Handler ui;
    private EditText serverInput;
    private EditText tokenInput;
    private TextView statusView;
    private TextView logView;
    private ScrollView logScroll;
    private Runnable poller;

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        ui = new Handler(Looper.getMainLooper());
        buildUi();
        requestNotifyPermission();
    }

    private int dp(int v) {
        return Math.round(getResources().getDisplayMetrics().density * v);
    }

    private void buildUi() {
        ScrollView rootScroll = new ScrollView(this);
        LinearLayout root = new LinearLayout(this);
        root.setOrientation(LinearLayout.VERTICAL);
        root.setPadding(dp(20), dp(20), dp(20), dp(20));
        rootScroll.addView(root);

        TextView title = new TextView(this);
        title.setText("iFlyGo 移动客户端");
        title.setTextSize(22);
        title.setTypeface(Typeface.DEFAULT_BOLD);
        root.addView(title);

        TextView subtitle = new TextView(this);
        subtitle.setText("完整 VPN 隧道（Android / HarmonyOS 4）");
        subtitle.setTextSize(13);
        subtitle.setTextColor(Color.GRAY);
        root.addView(subtitle);

        serverInput = new EditText(this);
        serverInput.setHint("控制面地址，如 https://iflygo.example.com");
        serverInput.setSingleLine(true);
        serverInput.setInputType(InputType.TYPE_CLASS_TEXT | InputType.TYPE_TEXT_VARIATION_URI);
        IflygoApp app = (IflygoApp) getApplication();
        serverInput.setText(app.server());
        root.addView(serverInput, new LinearLayout.LayoutParams(
                ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT));

        tokenInput = new EditText(this);
        tokenInput.setHint("enroll token（页面创建客户端后复制）");
        tokenInput.setSingleLine(true);
        tokenInput.setInputType(InputType.TYPE_CLASS_TEXT);
        root.addView(tokenInput, new LinearLayout.LayoutParams(
                ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT));

        LinearLayout buttons = new LinearLayout(this);
        buttons.setOrientation(LinearLayout.HORIZONTAL);
        buttons.setGravity(Gravity.CENTER_VERTICAL);
        Button enroll = new Button(this);
        enroll.setText("注册");
        enroll.setOnClickListener(v -> doEnroll());
        Button start = new Button(this);
        start.setText("启动");
        start.setOnClickListener(v -> doStart());
        Button stop = new Button(this);
        stop.setText("停止");
        stop.setOnClickListener(v -> doStop());
        Button refresh = new Button(this);
        refresh.setText("刷新");
        refresh.setOnClickListener(v -> doRefresh());
        buttons.addView(enroll);
        buttons.addView(start);
        buttons.addView(stop);
        buttons.addView(refresh);
        root.addView(buttons);

        statusView = new TextView(this);
        statusView.setTextSize(13);
        statusView.setTypeface(Typeface.MONOSPACE);
        statusView.setPadding(0, dp(12), 0, dp(8));
        root.addView(statusView);

        TextView logTitle = new TextView(this);
        logTitle.setText("运行日志");
        logTitle.setTextSize(13);
        logTitle.setTypeface(Typeface.DEFAULT_BOLD);
        root.addView(logTitle);

        logScroll = new ScrollView(this);
        logScroll.setBackgroundColor(Color.rgb(245, 245, 245));
        logScroll.setPadding(dp(8), dp(8), dp(8), dp(8));
        logView = new TextView(this);
        logView.setTextSize(10);
        logView.setTypeface(Typeface.MONOSPACE);
        logView.setTextColor(Color.DKGRAY);
        logScroll.addView(logView);
        LinearLayout.LayoutParams lp = new LinearLayout.LayoutParams(
                ViewGroup.LayoutParams.MATCH_PARENT, dp(240));
        lp.topMargin = dp(4);
        root.addView(logScroll, lp);

        setContentView(rootScroll);
    }

    private void requestNotifyPermission() {
        if (Build.VERSION.SDK_INT >= 33 &&
                checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED) {
            requestPermissions(new String[]{Manifest.permission.POST_NOTIFICATIONS}, REQ_NOTIFY);
        }
    }

    private String server() {
        return serverInput.getText().toString().trim();
    }

    private void saveServer() {
        IflygoApp app = (IflygoApp) getApplication();
        app.saveServer(server());
        app.client().setServer(server());
    }

    private void doEnroll() {
        final String s = server();
        final String t = tokenInput.getText().toString().trim();
        if (s.isEmpty() || t.isEmpty()) {
            toast("请填写控制面地址与 enroll token");
            return;
        }
        saveServer();
        io.execute(() -> {
            try {
                ((IflygoApp) getApplication()).client().enroll(t);
                ui.post(() -> {
                    tokenInput.setText("");
                    toast("注册成功");
                    refreshUi();
                });
            } catch (Exception e) {
                ui.post(() -> {
                    toast("注册失败: " + e.getMessage());
                    refreshUi();
                });
            }
        });
    }

    private void doRefresh() {
        saveServer();
        io.execute(() -> {
            try {
                Client client = ((IflygoApp) getApplication()).client();
                boolean wasRunning = new JSONObject(client.status()).optBoolean("running");
                client.refresh();
                if (wasRunning) {
                    Intent i = new Intent(MainActivity.this, IflygoVpnService.class);
                    i.setAction("restart");
                    if (Build.VERSION.SDK_INT >= 26) {
                        startForegroundService(i);
                    } else {
                        startService(i);
                    }
                }
                ui.post(() -> toast(wasRunning ? "配置已刷新，隧道已重启" : "配置已刷新"));
            } catch (Exception e) {
                ui.post(() -> toast("刷新失败: " + e.getMessage()));
            }
        });
    }

    private void doStart() {
        saveServer();
        Intent prep = VpnService.prepare(this);
        if (prep != null) {
            startActivityForResult(prep, REQ_VPN);
        } else {
            startVpn();
        }
    }

    private void startVpn() {
        Intent i = new Intent(this, IflygoVpnService.class);
        if (Build.VERSION.SDK_INT >= 26) {
            startForegroundService(i);
        } else {
            startService(i);
        }
    }

    private void doStop() {
        stopService(new Intent(this, IflygoVpnService.class));
        io.execute(() -> {
            try {
                ((IflygoApp) getApplication()).client().stop();
            } catch (Exception ignored) {
            }
            ui.post(this::refreshUi);
        });
    }

    @Override
    protected void onActivityResult(int requestCode, int resultCode, Intent data) {
        super.onActivityResult(requestCode, resultCode, data);
        if (requestCode == REQ_VPN && resultCode == RESULT_OK) {
            startVpn();
        }
    }

    private void refreshUi() {
        try {
            String raw = ((IflygoApp) getApplication()).client().status();
            JSONObject o = new JSONObject(raw);
            String text = "已注册: " + (o.optBoolean("enrolled") ? "是" : "否")
                    + "\n运行中: " + (o.optBoolean("running") ? "是" : "否")
                    + "\n节点: " + o.optString("name")
                    + "\n网络: " + o.optString("network")
                    + "\nIPv4: " + o.optString("ipv4")
                    + "\nIPv6: " + o.optString("ipv6")
                    + "\nDNS: " + o.optString("dns")
                    + "\nMTU: " + o.optInt("mtu", 0)
                    + "\n数据面: " + o.optString("data_plane_mode")
                    + "\n中继: " + o.optString("tcp_relay");
            String err = o.optString("error");
            if (!err.isEmpty()) {
                text += "\n错误: " + err;
            }
            statusView.setText(text);
            String logs = ((IflygoApp) getApplication()).client().logs();
            logView.setText(logs);
            logScroll.post(() -> logScroll.fullScroll(View.FOCUS_DOWN));
        } catch (Exception e) {
            statusView.setText("状态读取失败: " + e.getMessage());
        }
        if (poller != null) {
            ui.postDelayed(poller, 1000);
        }
    }

    @Override
    protected void onResume() {
        super.onResume();
        poller = this::refreshUi;
        ui.post(poller);
    }

    @Override
    protected void onPause() {
        super.onPause();
        if (poller != null) {
            ui.removeCallbacks(poller);
        }
    }

    private void toast(String text) {
        Toast.makeText(this, text, Toast.LENGTH_SHORT).show();
    }
}
