#!/usr/bin/env python3
"""前端合并：把 upgrade_mgr 前端作为主控制台，注入 iflygo 隧道管理视图。

用法（在仓库根目录）：
    python3 scripts/inject_tunnel_frontend.py

流程：
  1. 从 third_party/upgrade_mgr/web/dist 复制 index.html / assets / favicon 到 web/dist；
  2. 在 assets/app.js 中注入「隧道管理」下拉菜单 + 5 个视图（客户端/网络/节点/路由/域名）；
  3. 复制 quickstart.json。

upgrade_mgr 前端更新后，重新运行本脚本即可重新合并（隧道视图代码集中在本文件，
不随 upgrade_mgr 更新丢失）。
"""

import os
import re
import shutil

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
SRC = os.path.join(ROOT, "third_party", "upgrade_mgr", "web", "dist")
DST = os.path.join(ROOT, "web", "dist")

# 注入视图的补充样式（追加到 app.css 末尾）
EXTRA_CSS = """
/* iflygo 注入视图（隧道/DDNSTO/Liaison）补充样式 */
.form-row2 { display: flex; gap: 8px; flex-wrap: wrap; margin: 8px 0; }
.form-row2 input, .form-row2 select { flex: 1; min-width: 120px; }
.ssh-xterm { margin-top: 8px; }
.ssh-xterm .xterm { padding: 8px; border-radius: 8px; }
.file-content { background: #f5f2ec; padding: 12px; border-radius: 8px; overflow-x: auto; font-size: 12px; max-height: 360px; }
.vnc-wrap { margin-top: 8px; }

/* 隧道管理弹窗：表单随屏幕自适应，长文本/长地址自动换行，避免溢出 */
.tunnel-modal { max-width: 760px; width: 100%; }
.tunnel-modal .field { margin: 10px 0; }
.tunnel-modal .field label,
.tunnel-modal .modal-head h3 { overflow-wrap: anywhere; word-break: break-word; }
.tunnel-modal input, .tunnel-modal select {
  width: 100%; box-sizing: border-box; min-width: 0;
}
.tunnel-table { width: 100%; }
.tunnel-table th, .tunnel-table td {
  overflow-wrap: anywhere; word-break: break-word; vertical-align: top;
}
.tunnel-table .tunnel-name { min-width: 96px; }
.tunnel-table .tunnel-wrap { min-width: 110px; }
.tunnel-actions { display: flex; flex-wrap: wrap; gap: 6px; }
@media (max-width: 720px) {
  .tunnel-modal { max-width: 100%; padding: 12px; }
  .tunnel-table { font-size: 12px; }
  .tunnel-table .tunnel-name { min-width: 72px; }
  .tunnel-table .tunnel-wrap { min-width: 84px; }
}
@media (max-width: 520px) {
  .tunnel-actions { flex-direction: column; align-items: stretch; }
  .tunnel-actions .btn { width: 100%; }
}
"""

# ---------------------------------------------------------------------------
# 隧道管理视图 JS 片段（适配 upgrade_mgr 前端的 h()/api()/switchView() 约定）
# ---------------------------------------------------------------------------
TUNNEL_JS = r'''
  // ===== iflygo 隧道管理（inject_tunnel_frontend.py 注入，勿手改）=====
  state.tunnelSection = 'clients';
  state.tunnelNetworks = [];
  state.tunnelClients = [];
  state.tunnelServerNodes = [];
  state.tunnelRoutes = [];
  state.tunnelEdgeServices = [];
  state.tunnelRouteNetwork = 0;
  state.tunnelMsg = '';
  state.tunnelOk = false;
  state.tunnelLoaded = false;

  function tunnelSetMsg(ok, text) { state.tunnelMsg = text || ''; state.tunnelOk = !!ok; }
  function tunnelMsgEl() {
    if (!state.tunnelMsg) return null;
    return h('div', { class: state.tunnelOk ? 'hint' : 'err field', role: 'alert' }, [state.tunnelMsg]);
  }
  function tunnelReload() { return tunnelLoadAll().then(render); }
  function tunnelLoadAll() {
    return Promise.all([
      api('/api/networks').catch(function () { return []; }),
      api('/api/clients').catch(function () { return []; }),
      api('/api/server-nodes').catch(function () { return []; }),
      api('/api/edge-services').catch(function () { return []; })
    ]).then(function (r) {
      state.tunnelNetworks = r[0] || [];
      state.tunnelClients = r[1] || [];
      state.tunnelServerNodes = r[2] || [];
      state.tunnelEdgeServices = r[3] || [];
      return tunnelLoadRoutes();
    });
  }
  function tunnelLoadRoutes() {
    var nid = state.tunnelRouteNetwork || (state.tunnelNetworks[0] && state.tunnelNetworks[0].id) || 0;
    state.tunnelRouteNetwork = nid;
    if (!nid) { state.tunnelRoutes = []; return Promise.resolve(); }
    return api('/api/networks/' + nid + '/routes').then(function (d) {
      state.tunnelRoutes = d || [];
    }).catch(function () { state.tunnelRoutes = []; });
  }
  function tunnelNetSelect(value, onchange) {
    var opts = state.tunnelNetworks.map(function (n) {
      return h('option', { value: n.id }, [n.name + ' (' + (n.cidr4 || '') + ')']);
    });
    var sel = h('select', onchange ? { onchange: onchange } : {}, opts);
    if (value != null) sel.value = value;
    return sel;
  }
  function tunnelView() {
    if (!state.tunnelLoaded) { state.tunnelLoaded = true; tunnelLoadAll().then(render); }
    var body;
    switch (state.tunnelSection) {
      case 'networks': body = tunnelNetworksView(); break;
      case 'serverNodes': body = tunnelServerNodesView(); break;
      case 'routes': body = tunnelRoutesView(); break;
      case 'edgeServices': body = tunnelEdgeServicesView(); break;
      default: body = tunnelClientsView();
    }
    return h('div', {}, [
      h('div', { class: 'muted' }, ['管理 overlay 网络、客户端、服务端节点、不安全路由与边缘服务。']),
      tunnelMsgEl(),
      body
    ]);
  }

  // ---------- 通用弹窗表单（所有编辑/创建操作均在弹窗内完成）----------
  function tunnelCloseModal(mask) {
    if (mask && mask.parentNode) { mask.parentNode.removeChild(mask); }
  }
  function tunnelInput(attrs) { return h('input', attrs || {}, []); }
  function tunnelSelect(options, value) {
    var sel = h('select', {}, options.map(function (o) { return h('option', { value: o[0] }, [o[1]]); }));
    if (value != null) { sel.value = String(value); }
    return sel;
  }
  function tunnelFormModal(opts) {
    var errBox = h('div', { class: 'err field', style: 'display:none' }, []);
    var submit = h('button', { class: 'btn primary', type: 'button' }, [opts.submitText || '保存']);
    var cancel = h('button', { class: 'btn ghost', type: 'button' }, ['取消']);
    var children = [h('div', { class: 'modal-head' }, [h('h3', {}, [opts.title || '编辑']), cancel])];
    (opts.fields || []).forEach(function (f) {
      if (!f) return;
      children.push(field(f.label, f.input, f.hint));
    });
    if (opts.extra) { children.push(opts.extra); }
    children.push(errBox);
    children.push(h('div', { class: 'toolbar' }, [submit]));
    var mask = h('div', {
      class: 'modal-mask',
      onclick: function (e) { if (e.target === mask) { tunnelCloseModal(mask); } }
    }, [h('div', { class: 'modal tunnel-modal', role: 'dialog', 'aria-modal': 'true' }, children)]);
    cancel.onclick = function () { tunnelCloseModal(mask); };
    submit.onclick = function () {
      errBox.style.display = 'none';
      submit.disabled = true;
      var old = submit.textContent;
      submit.textContent = '保存中…';
      Promise.resolve().then(function () { return opts.onSubmit(); }).then(function () {
        tunnelCloseModal(mask);
        if (opts.after) { opts.after(); } else { render(); }
      }).catch(function (e) {
        submit.disabled = false;
        submit.textContent = old;
        errBox.textContent = (e && e.message) || String(e);
        errBox.style.display = '';
      });
    };
    document.body.appendChild(mask);
    if (opts.focus && opts.focus.focus) { opts.focus.focus(); }
    return mask;
  }
  function tunnelClientSettingsFields(cs) {
    cs = cs || {};
    var dnsMode = tunnelSelect([
      ['inherit', '跟随网络/全局默认'],
      ['on', '启用服务端 DNS'],
      ['off', '关闭服务端 DNS']
    ], cs.use_server_dns === true ? 'on' : (cs.use_server_dns === false ? 'off' : 'inherit'));
    var dns = tunnelInput({ placeholder: '如 10.77.0.1（留空跟随默认）', value: cs.dns || '' });
    var mtu = tunnelInput({ type: 'number', min: 576, max: 9000, placeholder: '0 = 跟随默认', value: cs.mtu || '' });
    var interval = tunnelInput({ placeholder: '如 30s（留空跟随默认）', value: cs.interval || '' });
    var mode = tunnelSelect([
      ['inherit', '跟随默认'], ['auto', 'auto'], ['tcp', 'tcp'], ['udp', 'udp']
    ], cs.data_plane_mode || 'inherit');
    return {
      fields: [
        { label: '服务端 DNS', input: dnsMode, hint: '可单独关闭该主机的服务端 DNS；关闭后不下发 DNS。' },
        { label: 'DNS（覆盖网络/全局）', input: dns },
        { label: '客户端 MTU（0=跟随默认）', input: mtu },
        { label: '心跳间隔（如 30s）', input: interval },
        { label: '数据面模式', input: mode }
      ],
      read: function () {
        var out = {};
        var dm = dnsMode.value;
        if (dm === 'on') { out.use_server_dns = true; }
        else if (dm === 'off') { out.use_server_dns = false; }
        out.dns = dns.value.trim();
        var m = Number(mtu.value || 0);
        if (m > 0) { out.mtu = m; }
        out.interval = interval.value.trim();
        if (mode.value !== 'inherit') { out.data_plane_mode = mode.value; }
        return out;
      }
    };
  }
  function tunnelClientSummary(c) {
    var cs = c.client_settings || {};
    var parts = [];
    if (cs.use_server_dns === false) { parts.push('DNS:关闭'); }
    else if (cs.use_server_dns === true) { parts.push('DNS:启用'); }
    if (cs.dns) { parts.push('DNS=' + cs.dns); }
    if (cs.mtu) { parts.push('MTU=' + cs.mtu); }
    if (cs.interval) { parts.push('心跳=' + cs.interval); }
    if (cs.data_plane_mode) { parts.push('数据面=' + cs.data_plane_mode); }
    return parts.length ? parts.join(' ') : '跟随网络/全局';
  }
  function tunnelNetName(id) {
    for (var i = 0; i < state.tunnelNetworks.length; i++) {
      if (state.tunnelNetworks[i].id === id) { return state.tunnelNetworks[i].name; }
    }
    return '#' + id;
  }

  function tunnelClientsView() {
    var rows = state.tunnelClients.map(function (c) {
      return h('tr', {}, [
        h('td', { class: 'tunnel-name' }, [c.name || '']),
        h('td', {}, [c.ipv4 || '—']),
        h('td', {}, [c.ipv6 || '—']),
        h('td', { class: 'tunnel-wrap' }, [tunnelNetName(c.network_id)]),
        h('td', { class: 'tunnel-wrap' }, [tunnelClientSummary(c)]),
        h('td', {}, [c.status || '—']),
        h('td', {}, [h('div', { class: 'tunnel-actions' }, [
          h('button', { class: 'btn sm primary', onclick: function () { tunnelOpenClientEdit(c); } }, ['编辑']),
          h('button', { class: 'btn sm', onclick: function () { tunnelGetToken(c.id, c.name); } }, ['安装命令']),
          h('button', { class: 'btn sm', onclick: function () { tunnelRenewCert(c.id); } }, ['续期']),
          h('button', { class: 'btn sm', onclick: function () { tunnelRevokeCert(c.id); } }, ['吊销']),
          h('button', { class: 'btn sm danger', onclick: function () { tunnelDelClient(c.id); } }, ['删除'])
        ])])
      ]);
    });
    return h('div', { class: 'card' }, [
      h('div', { class: 'card-head' }, [
        h('h3', {}, ['客户端']),
        h('button', { class: 'btn primary', onclick: tunnelOpenClientCreate }, ['新建客户端'])
      ]),
      h('div', { class: 'muted' }, ['每台主机可单独配置 DNS/MTU/心跳/数据面；修改 IP 后自动重签证书并下发，客户端无需手工改文件。']),
      h('div', { class: 'table-wrap' }, [
        h('table', { class: 'tunnel-table' }, [
          h('thead', {}, [h('tr', {}, [h('th', {}, ['名称']), h('th', {}, ['IPv4']), h('th', {}, ['IPv6']), h('th', {}, ['网络']), h('th', {}, ['客户端参数']), h('th', {}, ['状态']), h('th', {}, ['操作'])])]),
          h('tbody', {}, rows.length ? rows : [h('tr', {}, [h('td', { colspan: 7, class: 'muted' }, ['暂无客户端'])])])
        ])
      ])
    ]);
  }

  function tunnelOpenClientCreate() {
    if (!state.tunnelNetworks.length) {
      tunnelSetMsg(false, '请先创建网络');
      render();
      return;
    }
    var net = tunnelNetSelect(state.tunnelNetworks[0].id);
    var name = tunnelInput({ placeholder: '名称' });
    var ipv4 = tunnelInput({ placeholder: 'IPv4（留空自动分配）' });
    var ipv6 = tunnelInput({ placeholder: 'IPv6（留空自动分配）' });
    var groups = tunnelInput({ placeholder: 'Nebula 分组（逗号分隔，可选）' });
    var subnets = tunnelInput({ placeholder: '额外 unsafe 子网（逗号分隔，可选）' });
    var cs = tunnelClientSettingsFields({});
    tunnelFormModal({
      title: '新建客户端',
      fields: [
        { label: '所属网络', input: net },
        { label: '名称', input: name },
        { label: 'IPv4', input: ipv4, hint: '留空自动从网络网段分配。' },
        { label: 'IPv6', input: ipv6 },
        { label: '分组', input: groups },
        { label: '子网', input: subnets }
      ].concat(cs.fields),
      submitText: '创建客户端',
      focus: name,
      onSubmit: function () {
        return api('/api/clients', {
          method: 'POST',
          body: JSON.stringify({
            network_id: Number(net.value), name: name.value.trim(),
            ipv4: ipv4.value.trim(), ipv6: ipv6.value.trim(),
            groups: groups.value.trim(), subnets: subnets.value.trim(),
            client_settings: cs.read()
          })
        }).then(function (d) {
          var cmd = (d && d.install_command) || '';
          tunnelSetMsg(true, '客户端已创建' + (cmd ? '，安装命令已复制' : ''));
          if (cmd && navigator.clipboard) { navigator.clipboard.writeText(cmd).catch(function () {}); }
        });
      },
      after: function () { tunnelLoadAll().then(render); }
    });
  }

  function tunnelOpenClientEdit(c) {
    var name = tunnelInput({ value: c.name || '' });
    var ipv4 = tunnelInput({ value: c.ipv4 || '', placeholder: '留空则自动分配新地址' });
    var ipv6 = tunnelInput({ value: c.ipv6 || '' });
    var groups = tunnelInput({ value: c.groups || '' });
    var subnets = tunnelInput({ value: c.subnets || '' });
    var cs = tunnelClientSettingsFields(c.client_settings || {});
    tunnelFormModal({
      title: '编辑客户端 · ' + (c.name || ('#' + c.id)),
      fields: [
        { label: '名称', input: name },
        { label: 'IPv4', input: ipv4, hint: '修改后会重新签发证书并自动下发，无需手工操作客户端。' },
        { label: 'IPv6', input: ipv6 },
        { label: '分组', input: groups },
        { label: '子网', input: subnets }
      ].concat(cs.fields),
      submitText: '保存',
      focus: name,
      onSubmit: function () {
        return api('/api/clients/' + c.id, {
          method: 'PUT',
          body: JSON.stringify({
            name: name.value.trim(), ipv4: ipv4.value.trim(), ipv6: ipv6.value.trim(),
            groups: groups.value.trim(), subnets: subnets.value.trim(),
            client_settings: cs.read()
          })
        }).then(function () {
          tunnelSetMsg(true, '已保存，客户端下次心跳/刷新自动应用新配置');
        });
      },
      after: function () { tunnelLoadAll().then(render); }
    });
  }

  function tunnelOpenNetworkCreate() {
    var name = tunnelInput({ placeholder: '名称' });
    var cidr4 = tunnelInput({ placeholder: 'IPv4 CIDR，如 10.77.0.0/16' });
    var cidr6 = tunnelInput({ placeholder: 'IPv6 CIDR（可选）' });
    var gw4 = tunnelInput({ placeholder: 'IPv4 网关（可选）' });
    var gw6 = tunnelInput({ placeholder: 'IPv6 网关（可选）' });
    tunnelFormModal({
      title: '新建网络',
      fields: [
        { label: '名称', input: name },
        { label: 'IPv4 CIDR', input: cidr4 },
        { label: 'IPv6 CIDR', input: cidr6 },
        { label: 'IPv4 网关', input: gw4 },
        { label: 'IPv6 网关', input: gw6 }
      ],
      submitText: '创建网络',
      focus: name,
      onSubmit: function () {
        return api('/api/networks', {
          method: 'POST',
          body: JSON.stringify({ name: name.value.trim(), cidr4: cidr4.value.trim(), cidr6: cidr6.value.trim(), gateway4: gw4.value.trim(), gateway6: gw6.value.trim() })
        }).then(function () { tunnelSetMsg(true, '网络已创建'); });
      },
      after: function () { tunnelLoadAll().then(render); }
    });
  }

  function tunnelOpenNetworkEdit(n) {
    api('/api/networks/' + n.id).then(function (d) {
      var nb = (d && d.nebula) || {};
      var cur = d || n;
      function numOpt(v) { return h('option', { value: String(v) }, [String(v)]); }
      var cipher = tunnelSelect([['', '默认 aes-256-gcm'], ['aes-256-gcm', 'aes-256-gcm'], ['chacha20-poly1305', 'chacha20-poly1305']], nb.cipher || '');
      var listenPort = tunnelInput({ type: 'number', value: nb.listen_port || '' });
      var lhInterval = tunnelInput({ type: 'number', value: nb.lighthouse_interval || '' });
      var punch = h('input', { type: 'checkbox', checked: !!nb.punch }, []);
      var punchRespond = h('input', { type: 'checkbox', checked: !!nb.punch_respond }, []);
      var punchDelay = tunnelInput({ value: nb.punch_delay || '' });
      var useRelays = h('input', { type: 'checkbox', checked: !!nb.use_relays }, []);
      var relays = tunnelInput({ value: (nb.relays || []).join(', ') });
      var tunDevice = tunnelInput({ value: nb.tun_device || '' });
      var tunDisabled = h('input', { type: 'checkbox', checked: !!nb.tun_disabled }, []);
      var prefRanges = tunnelInput({ value: (nb.preferred_ranges || []).join(', ') });
      var hsInterval = tunnelInput({ value: nb.handshake_try_interval || '' });
      var hsRetries = tunnelInput({ type: 'number', value: nb.handshake_retries || '' });
      var logLevel = tunnelSelect([['', '默认 info'], ['error', 'error'], ['warn', 'warn'], ['info', 'info'], ['debug', 'debug']], nb.log_level || '');
      var logFormat = tunnelSelect([['', '默认 text'], ['text', 'text'], ['json', 'json']], nb.log_format || '');
      var statsType = tunnelSelect([['', '关闭'], ['prometheus', 'prometheus'], ['graphite', 'graphite']], nb.stats_type || '');
      var statsListen = tunnelInput({ value: nb.stats_listen || '' });
      var dns = tunnelInput({ value: cur ? (cur.dns || '') : '', placeholder: '如 1.1.1.1,8.8.8.8；留空跟随全局' });
      var mtu = tunnelInput({ type: 'number', value: cur ? (cur.mtu || 1300) : 1300 });
      tunnelFormModal({
        title: '网络设置 · ' + (n.name || ('#' + n.id)),
        fields: [
          { label: 'DNS（网络级，客户端下发）', input: dns },
          { label: 'MTU（网络级）', input: mtu },
          { label: '加密算法', input: cipher },
          { label: '监听端口', input: listenPort },
          { label: 'Lighthouse 间隔(s)', input: lhInterval },
          { label: 'NAT 打洞 punch', input: punch },
          { label: '响应打洞 punch_respond', input: punchRespond },
          { label: '打洞延迟 punch_delay', input: punchDelay },
          { label: '使用中继 use_relays', input: useRelays },
          { label: '中继地址 relays（逗号分隔）', input: relays },
          { label: 'TUN 设备', input: tunDevice },
          { label: '禁用 TUN（仅测试）', input: tunDisabled },
          { label: '优先网段 preferred_ranges', input: prefRanges },
          { label: '握手间隔', input: hsInterval },
          { label: '握手重试次数', input: hsRetries },
          { label: '日志级别', input: logLevel },
          { label: '日志格式', input: logFormat },
          { label: '统计类型', input: statsType },
          { label: '统计监听地址', input: statsListen }
        ],
        submitText: '保存网络设置',
        onSubmit: function () {
          return api('/api/networks/' + n.id, {
            method: 'PUT',
            body: JSON.stringify({
              dns: dns.value.trim(),
              mtu: Number(mtu.value || 0),
              nebula: {
                cipher: cipher.value,
                listen_port: Number(listenPort.value || 0),
                lighthouse_interval: Number(lhInterval.value || 0),
                punch: punch.checked,
                punch_respond: punchRespond.checked,
                punch_delay: punchDelay.value,
                am_relay: !!nb.am_relay,
                use_relays: useRelays.checked,
                relays: tunnelNebulaList(relays.value),
                tun_device: tunDevice.value,
                tun_disabled: tunDisabled.checked,
                preferred_ranges: tunnelNebulaList(prefRanges.value),
                handshake_try_interval: hsInterval.value,
                handshake_retries: Number(hsRetries.value || 0),
                log_level: logLevel.value,
                log_format: logFormat.value,
                stats_type: statsType.value,
                stats_listen: statsListen.value
              }
            })
          }).then(function () { tunnelSetMsg(true, '网络/Nebula 设置已保存，客户端下次心跳自动拉取'); });
        },
        after: function () { tunnelLoadAll().then(render); }
      });
    }).catch(function (e) { tunnelSetMsg(false, e.message); render(); });
  }

  function tunnelNetworksView() {
    var rows = state.tunnelNetworks.map(function (n) {
      return h('tr', {}, [
        h('td', { class: 'tunnel-name' }, [n.name || '']),
        h('td', {}, [n.cidr4 || '']),
        h('td', {}, [n.cidr6 || '—']),
        h('td', {}, [String(n.mtu || '')]),
        h('td', {}, [h('div', { class: 'tunnel-actions' }, [
          h('button', { class: 'btn sm primary', onclick: function () { tunnelOpenNetworkEdit(n); } }, ['编辑']),
          h('button', { class: 'btn sm danger', onclick: function () { tunnelDelNetwork(n.id); } }, ['删除'])
        ])])
      ]);
    });
    return h('div', { class: 'card' }, [
      h('div', { class: 'card-head' }, [
        h('h3', {}, ['网络']),
        h('button', { class: 'btn primary', onclick: tunnelOpenNetworkCreate }, ['新建网络'])
      ]),
      h('div', { class: 'muted' }, ['网络级 Nebula 参数在每行「编辑」弹窗中配置；保存后该网络下客户端自动拉取。']),
      h('div', { class: 'table-wrap' }, [
        h('table', { class: 'tunnel-table' }, [
          h('thead', {}, [h('tr', {}, [h('th', {}, ['名称']), h('th', {}, ['IPv4']), h('th', {}, ['IPv6']), h('th', {}, ['MTU']), h('th', {}, ['操作'])])]),
          h('tbody', {}, rows.length ? rows : [h('tr', {}, [h('td', { colspan: 5, class: 'muted' }, ['暂无网络'])])])
        ])
      ])
    ]);
  }

  function tunnelOpenServerNodeCreate() {
    if (!state.tunnelNetworks.length) {
      tunnelSetMsg(false, '请先创建网络');
      render();
      return;
    }
    var net = tunnelNetSelect(state.tunnelNetworks[0].id);
    var name = tunnelInput({ placeholder: '名称' });
    var roles = tunnelInput({ placeholder: '角色，如 lighthouse,relay' });
    var region = tunnelInput({ placeholder: '区域（可选）' });
    var ip4 = tunnelInput({ placeholder: 'overlay IPv4（留空自动）' });
    var ip6 = tunnelInput({ placeholder: 'overlay IPv6（留空自动）' });
    var host = tunnelInput({ placeholder: '公网 host' });
    var port = tunnelInput({ placeholder: '公网端口', type: 'number' });
    tunnelFormModal({
      title: '新建服务端节点',
      fields: [
        { label: '所属网络', input: net },
        { label: '名称', input: name },
        { label: '角色', input: roles },
        { label: '区域', input: region },
        { label: 'overlay IPv4', input: ip4 },
        { label: 'overlay IPv6', input: ip6 },
        { label: '公网 host', input: host },
        { label: '公网端口', input: port }
      ],
      submitText: '创建节点',
      focus: name,
      onSubmit: function () {
        return api('/api/server-nodes', {
          method: 'POST',
          body: JSON.stringify({ network_id: Number(net.value), name: name.value.trim(), roles: roles.value.trim(), region: region.value.trim(), overlay_ip4: ip4.value.trim(), overlay_ip6: ip6.value.trim(), public_host: host.value.trim(), public_port: Number(port.value || 0) })
        }).then(function () { tunnelSetMsg(true, '节点已创建'); });
      },
      after: function () { tunnelLoadAll().then(render); }
    });
  }

  function tunnelOpenServerNodeEdit(n) {
    var name = tunnelInput({ value: n.name || '' });
    var roles = tunnelInput({ value: n.roles || '' });
    var region = tunnelInput({ value: n.region || '' });
    var ip4 = tunnelInput({ value: n.overlay_ip4 || '' });
    var ip6 = tunnelInput({ value: n.overlay_ip6 || '' });
    var host = tunnelInput({ value: n.public_host || '' });
    var port = tunnelInput({ type: 'number', value: n.public_port || '' });
    tunnelFormModal({
      title: '编辑服务端节点 · ' + (n.name || ('#' + n.id)),
      fields: [
        { label: '名称', input: name },
        { label: '角色', input: roles },
        { label: '区域', input: region },
        { label: 'overlay IPv4', input: ip4 },
        { label: 'overlay IPv6', input: ip6 },
        { label: '公网 host', input: host },
        { label: '公网端口', input: port }
      ],
      submitText: '保存',
      focus: name,
      onSubmit: function () {
        return api('/api/server-nodes/' + n.id, {
          method: 'PUT',
          body: JSON.stringify({ name: name.value.trim(), roles: roles.value.trim(), region: region.value.trim(), overlay_ip4: ip4.value.trim(), overlay_ip6: ip6.value.trim(), public_host: host.value.trim(), public_port: Number(port.value || 0) })
        }).then(function () { tunnelSetMsg(true, '节点已保存'); });
      },
      after: function () { tunnelLoadAll().then(render); }
    });
  }

  function tunnelServerNodesView() {
    var rows = state.tunnelServerNodes.map(function (n) {
      return h('tr', {}, [
        h('td', { class: 'tunnel-name' }, [n.name || '']),
        h('td', { class: 'tunnel-wrap' }, [n.roles || '']),
        h('td', {}, [n.overlay_ip4 || '—']),
        h('td', { class: 'tunnel-wrap' }, [(n.public_host || '') + (n.public_port ? ':' + n.public_port : '')]),
        h('td', {}, [h('div', { class: 'tunnel-actions' }, [
          h('button', { class: 'btn sm primary', onclick: function () { tunnelOpenServerNodeEdit(n); } }, ['编辑']),
          h('button', { class: 'btn sm danger', onclick: function () { tunnelDelServerNode(n.id); } }, ['删除'])
        ])])
      ]);
    });
    return h('div', { class: 'card' }, [
      h('div', { class: 'card-head' }, [
        h('h3', {}, ['服务端节点（lighthouse/relay/edge/gateway）']),
        h('button', { class: 'btn primary', onclick: tunnelOpenServerNodeCreate }, ['新建节点'])
      ]),
      h('div', { class: 'table-wrap' }, [
        h('table', { class: 'tunnel-table' }, [
          h('thead', {}, [h('tr', {}, [h('th', {}, ['名称']), h('th', {}, ['角色']), h('th', {}, ['overlay IP']), h('th', {}, ['公网入口']), h('th', {}, ['操作'])])]),
          h('tbody', {}, rows.length ? rows : [h('tr', {}, [h('td', { colspan: 5, class: 'muted' }, ['暂无节点'])])])
        ])
      ])
    ]);
  }

  function tunnelGatewaysText(route) {
    return (route.gateways || []).map(function (g) { return g.overlay_ip + ':' + (g.weight || 1); }).join(', ');
  }
  function tunnelParseGateways(text) {
    return String(text || '').split(',').map(function (s) {
      var parts = s.trim().split(':');
      if (!parts[0]) { return null; }
      return { overlay_ip: parts[0].trim(), weight: Number(parts[1] || 1), priority: 0 };
    }).filter(function (g) { return g && g.overlay_ip; });
  }
  function tunnelRouteFields(route) {
    var cidr = tunnelInput({ placeholder: 'CIDR，如 10.8.1.0/24', value: route ? (route.cidr || '') : '' });
    var gws = tunnelInput({ placeholder: '网关 ip:weight,ip:weight', value: route ? tunnelGatewaysText(route) : '' });
    var mode = tunnelSelect([['active_active', 'active_active'], ['active_passive', 'active_passive']], route ? (route.mode || 'active_active') : 'active_active');
    var mtu = tunnelInput({ type: 'number', value: route ? (route.mtu || 1300) : 1300 });
    return {
      fields: [
        { label: 'CIDR', input: cidr },
        { label: '网关', input: gws, hint: '格式 ip:weight，多个用逗号分隔。' },
        { label: '模式', input: mode },
        { label: 'MTU', input: mtu }
      ],
      read: function () { return { cidr: cidr.value.trim(), mtu: Number(mtu.value || 1300), mode: mode.value, gateways: tunnelParseGateways(gws.value) }; }
    };
  }
  function tunnelOpenRouteCreate() {
    if (!state.tunnelRouteNetwork) {
      tunnelSetMsg(false, '请先选择网络');
      render();
      return;
    }
    var f = tunnelRouteFields(null);
    tunnelFormModal({
      title: '新建不安全路由',
      fields: f.fields,
      submitText: '创建路由',
      onSubmit: function () {
        return api('/api/networks/' + state.tunnelRouteNetwork + '/routes', { method: 'POST', body: JSON.stringify(f.read()) })
          .then(function () { tunnelSetMsg(true, '路由已创建'); });
      },
      after: function () { tunnelLoadRoutes().then(render); }
    });
  }
  function tunnelOpenRouteEdit(rt) {
    var f = tunnelRouteFields(rt);
    tunnelFormModal({
      title: '编辑路由 · ' + (rt.cidr || ('#' + rt.id)),
      fields: f.fields,
      submitText: '保存',
      onSubmit: function () {
        return api('/api/routes/' + rt.id, { method: 'PUT', body: JSON.stringify(f.read()) })
          .then(function () { tunnelSetMsg(true, '路由已保存'); });
      },
      after: function () { tunnelLoadRoutes().then(render); }
    });
  }

  function tunnelRoutesView() {
    var netSel = tunnelNetSelect(state.tunnelRouteNetwork, function () {
      state.tunnelRouteNetwork = Number(netSel.value);
      tunnelLoadRoutes().then(render);
    });
    var rows = state.tunnelRoutes.map(function (rt) {
      var gws = tunnelGatewaysText(rt);
      return h('tr', {}, [
        h('td', { class: 'tunnel-name' }, [rt.cidr || '']),
        h('td', {}, [rt.mode || '']),
        h('td', { class: 'tunnel-wrap' }, [gws || '—']),
        h('td', {}, [h('div', { class: 'tunnel-actions' }, [
          h('button', { class: 'btn sm primary', onclick: function () { tunnelOpenRouteEdit(rt); } }, ['编辑']),
          h('button', { class: 'btn sm danger', onclick: function () { tunnelDelRoute(rt.id); } }, ['删除'])
        ])])
      ]);
    });
    return h('div', { class: 'card' }, [
      h('div', { class: 'card-head' }, [
        h('h3', {}, ['不安全路由']),
        h('button', { class: 'btn primary', onclick: tunnelOpenRouteCreate }, ['新建路由'])
      ]),
      field('所属网络', netSel),
      h('div', { class: 'table-wrap' }, [
        h('table', { class: 'tunnel-table' }, [
          h('thead', {}, [h('tr', {}, [h('th', {}, ['CIDR']), h('th', {}, ['模式']), h('th', {}, ['网关(权重)']), h('th', {}, ['操作'])])]),
          h('tbody', {}, rows.length ? rows : [h('tr', {}, [h('td', { colspan: 4, class: 'muted' }, ['暂无路由'])])])
        ])
      ])
    ]);
  }

  function tunnelEdgeFields(s) {
    var net = tunnelNetSelect(s ? s.network_id : (state.tunnelNetworks[0] ? state.tunnelNetworks[0].id : 0));
    var name = tunnelInput({ placeholder: '名称', value: s ? (s.name || '') : '' });
    var proto = tunnelSelect([['http', 'http'], ['tcp', 'tcp']], s ? (s.protocol || 'http') : 'http');
    var host = tunnelInput({ placeholder: '域名（http），如 nas.example.com', value: s ? (s.host || '') : '' });
    var pubPort = tunnelInput({ placeholder: '公网端口（tcp）', type: 'number', value: s ? (s.public_port || '') : '' });
    var target = tunnelInput({ placeholder: '目标，如 10.77.0.2:5000', value: s ? (s.target || '') : '' });
    return {
      fields: [
        { label: '所属网络', input: net }, { label: '名称', input: name }, { label: '协议', input: proto },
        { label: '域名', input: host }, { label: '公网端口', input: pubPort }, { label: '目标', input: target }
      ],
      read: function () {
        return { network_id: Number(net.value), name: name.value.trim(), host: host.value.trim(), target: target.value.trim(), protocol: proto.value, public_port: Number(pubPort.value || 0) };
      }
    };
  }
  function tunnelOpenEdgeCreate() {
    if (!state.tunnelNetworks.length) {
      tunnelSetMsg(false, '请先创建网络');
      render();
      return;
    }
    var f = tunnelEdgeFields(null);
    tunnelFormModal({
      title: '新建边缘服务',
      fields: f.fields,
      submitText: '创建服务',
      onSubmit: function () {
        return api('/api/edge-services', { method: 'POST', body: JSON.stringify(f.read()) })
          .then(function () { tunnelSetMsg(true, '服务已创建'); });
      },
      after: function () { tunnelLoadAll().then(render); }
    });
  }
  function tunnelOpenEdgeEdit(s) {
    var f = tunnelEdgeFields(s);
    tunnelFormModal({
      title: '编辑边缘服务 · ' + (s.name || ('#' + s.id)),
      fields: f.fields,
      submitText: '保存',
      onSubmit: function () {
        return api('/api/edge-services/' + s.id, { method: 'PUT', body: JSON.stringify(f.read()) })
          .then(function () { tunnelSetMsg(true, '服务已保存'); });
      },
      after: function () { tunnelLoadAll().then(render); }
    });
  }

  function tunnelEdgeServicesView() {
    var rows = state.tunnelEdgeServices.map(function (s) {
      return h('tr', {}, [
        h('td', { class: 'tunnel-name' }, [s.name || '']),
        h('td', { class: 'tunnel-wrap' }, [s.protocol === 'tcp' ? ':' + (s.public_port || '') : (s.host || '')]),
        h('td', { class: 'tunnel-wrap' }, [s.target || '']),
        h('td', {}, [s.protocol || '']),
        h('td', {}, [h('div', { class: 'tunnel-actions' }, [
          h('button', { class: 'btn sm primary', onclick: function () { tunnelOpenEdgeEdit(s); } }, ['编辑']),
          h('button', { class: 'btn sm danger', onclick: function () { tunnelDelEdgeService(s.id); } }, ['删除'])
        ])])
      ]);
    });
    return h('div', { class: 'card' }, [
      h('div', { class: 'card-head' }, [
        h('h3', {}, ['边缘服务（自定义域名 / TCP 端口）']),
        h('button', { class: 'btn primary', onclick: tunnelOpenEdgeCreate }, ['新建服务'])
      ]),
      h('div', { class: 'table-wrap' }, [
        h('table', { class: 'tunnel-table' }, [
          h('thead', {}, [h('tr', {}, [h('th', {}, ['名称']), h('th', {}, ['入口']), h('th', {}, ['目标']), h('th', {}, ['协议']), h('th', {}, ['操作'])])]),
          h('tbody', {}, rows.length ? rows : [h('tr', {}, [h('td', { colspan: 5, class: 'muted' }, ['暂无服务'])])])
        ])
      ])
    ]);
  }

  function tunnelGetToken(id, name) {
    api('/api/clients/' + id + '/enroll-token', { method: 'POST' })
      .then(function (d) {
        tunnelSetMsg(true, name + ' 安装命令：' + (d.install_command || ''));
        if (navigator.clipboard) { navigator.clipboard.writeText(d.install_command).catch(function () {}); }
        render();
      })
      .catch(function (e) { tunnelSetMsg(false, e.message); render(); });
  }
  function tunnelRenewCert(id) {
    if (!window.confirm('确认续期该客户端证书？')) return;
    api('/api/clients/' + id + '/cert/renew', { method: 'POST' })
      .then(function () { tunnelSetMsg(true, '证书已续期'); render(); })
      .catch(function (e) { tunnelSetMsg(false, e.message); render(); });
  }
  function tunnelRevokeCert(id) {
    if (!window.confirm('确认吊销该客户端证书（加入 blocklist）？')) return;
    api('/api/clients/' + id + '/revoke', { method: 'POST' })
      .then(function () { tunnelSetMsg(true, '证书已吊销'); render(); })
      .catch(function (e) { tunnelSetMsg(false, e.message); render(); });
  }
  function tunnelDelClient(id) {
    if (!window.confirm('确认删除该客户端？')) return;
    api('/api/clients/' + id, { method: 'DELETE' }).then(tunnelReload).catch(function (e) { tunnelSetMsg(false, e.message); render(); });
  }
  function tunnelDelNetwork(id) {
    if (!window.confirm('确认删除该网络及其所有资源？')) return;
    api('/api/networks/' + id, { method: 'DELETE' }).then(tunnelReload).catch(function (e) { tunnelSetMsg(false, e.message); render(); });
  }
  function tunnelDelServerNode(id) {
    if (!window.confirm('确认删除该节点？')) return;
    api('/api/server-nodes/' + id, { method: 'DELETE' }).then(tunnelReload).catch(function (e) { tunnelSetMsg(false, e.message); render(); });
  }
  function tunnelDelRoute(id) {
    if (!window.confirm('确认删除该路由？')) return;
    api('/api/routes/' + id, { method: 'DELETE' }).then(function () { return tunnelLoadRoutes().then(render); }).catch(function (e) { tunnelSetMsg(false, e.message); render(); });
  }
  function tunnelDelEdgeService(id) {
    if (!window.confirm('确认删除该服务？')) return;
    api('/api/edge-services/' + id, { method: 'DELETE' }).then(tunnelReload).catch(function (e) { tunnelSetMsg(false, e.message); render(); });
  }
'''

# 导航下拉菜单（插入到「帮助」菜单前）
# ---------------------------------------------------------------------------
# DDNSTO 远程访问 + Liaison 远程管理套件视图 JS 片段
# ---------------------------------------------------------------------------
REMOTE_JS = r'''
  // ===== DDNSTO 远程访问 + Liaison 远程管理（inject_tunnel_frontend.py 注入）=====
  state.ddnsto = [];
  state.ddnstoClients = [];
  state.ddnstoLoading = false;
  state.liaisonSection = 'files';
  state.liaisonFiles = [];
  state.liaisonFilesPath = '/tmp';
  state.liaisonFileContent = '';
  state.liaisonDB = [];
  state.liaisonDBQuery = '';
  state.liaisonDBResult = null;
  state.liaisonPolicies = [];
  state.liaisonLLMModels = null;
  state.liaisonLLMChat = '';
  state.liaisonLLMReply = '';
  state.remoteMsg = '';
  state.remoteOk = false;
  state.remoteLoaded = {};

  function remoteSetMsg(ok, text) { state.remoteMsg = text || ''; state.remoteOk = !!ok; }
  function remoteMsgEl() {
    if (!state.remoteMsg) return null;
    return h('div', { class: state.remoteOk ? 'hint' : 'err field', role: 'alert' }, [state.remoteMsg]);
  }

  // ---------- DDNSTO ----------
  function loadDDNSTO() {
    state.ddnstoLoading = true;
    return Promise.all([
      api('/api/ddnsto/access').catch(function () { return []; }),
      api('/api/clients').catch(function () { return []; })
    ]).then(function (r) {
      state.ddnsto = r[0] || [];
      state.ddnstoClients = r[1] || [];
      state.ddnstoLoading = false;
    });
  }
  function ddnstoView() {
    var rows = state.ddnsto.map(function (a) {
      return h('tr', {}, [
        h('td', {}, [a.name || '']),
        h('td', {}, [a.host || '']),
        h('td', {}, [a.target || '']),
        h('td', {}, [a.protocol || '']),
        h('td', {}, [a.enabled ? '启用' : '停用']),
        h('td', {}, [h('button', { class: 'btn sm danger', onclick: function () { ddnstoDel(a.id); } }, ['删除'])])
      ]);
    });
    var clientSel = h('select', { value: state.ddnstoClients.length ? state.ddnstoClients[state.ddnstoClients.length - 1].id : '' }, state.ddnstoClients.map(function (c) {
      return h('option', { value: c.id }, [c.name + ' (' + (c.ipv4 || '未分配') + ')']);
    }));
    var port = h('input', { placeholder: '目标端口，如 5000', type: 'number' }, []);
    var proto = h('select', {}, [h('option', { value: 'http' }, ['http']), h('option', { value: 'tcp' }, ['tcp'])]);
    var create = h('button', { class: 'btn primary', onclick: function () {
      api('/api/ddnsto/access', { method: 'POST', body: JSON.stringify({ client_id: Number(clientSel.value), port: Number(port.value || 80), protocol: proto.value }) })
        .then(function (a) { remoteSetMsg(true, '已开通远程访问：' + a.host); return loadDDNSTO().then(render); })
        .catch(function (e) { remoteSetMsg(false, e.message); render(); });
    } }, ['开通远程访问']);
    return h('div', { class: 'card' }, [
      h('div', { class: 'card-head' }, [h('h3', {}, ['远程访问（DDNSTO）'])]),
      h('div', { class: 'muted' }, ['给客户端自动分配子域名，通过 edge 数据面反向代理到 overlay 目标（无带宽套餐限制）。']),
      h('div', { class: 'table-wrap' }, [
        h('table', {}, [
          h('thead', {}, [h('tr', {}, [h('th', {}, ['名称']), h('th', {}, ['子域名']), h('th', {}, ['目标']), h('th', {}, ['协议']), h('th', {}, ['状态']), h('th', {}, ['操作'])])]),
          h('tbody', {}, rows)
        ])
      ]),
      field('客户端', clientSel), field('目标端口', port), field('协议', proto), create
    ]);
  }
  function ddnstoDel(id) {
    if (!window.confirm('确认删除该远程访问？')) return;
    api('/api/ddnsto/access/' + id, { method: 'DELETE' }).then(function () { remoteSetMsg(true, '已删除'); return loadDDNSTO().then(render); }).catch(function (e) { remoteSetMsg(false, e.message); render(); });
  }

  // ---------- Liaison ----------
  function loadLiaison() {
    var jobs = [];
    if (state.liaisonSection === 'files') {
      jobs.push(api('/api/liaison/files?path=' + encodeURIComponent(state.liaisonFilesPath)).then(function (d) { state.liaisonFiles = d || []; }).catch(function () { state.liaisonFiles = []; }));
    }
    if (state.liaisonSection === 'db') {
      jobs.push(api('/api/liaison/db/connections').then(function (d) { state.liaisonDB = d || []; }).catch(function () { state.liaisonDB = []; }));
    }
    if (state.liaisonSection === 'policies') {
      jobs.push(api('/api/liaison/policies').then(function (d) { state.liaisonPolicies = (d && d.policies) || []; }).catch(function () { state.liaisonPolicies = []; }));
    }
    if (state.liaisonSection === 'llm') {
      jobs.push(api('/api/liaison/llm/models').then(function (d) { state.liaisonLLMModels = d || null; }).catch(function () { state.liaisonLLMModels = null; }));
    }
    return Promise.all(jobs);
  }
  function liaisonView() {
    if (!state.remoteLoaded[state.liaisonSection]) {
      state.remoteLoaded[state.liaisonSection] = true;
      loadLiaison().then(render);
    }
    var body;
    switch (state.liaisonSection) {
      case 'db': body = liaisonDBView(); break;
      case 'llm': body = liaisonLLMView(); break;
      case 'policies': body = liaisonPoliciesView(); break;
      case 'ssh': body = liaisonSSHView(); break;
      case 'vnc': body = liaisonVNCView(); break;
      case 'rdp': body = liaisonRDPView(); break;
      default: body = liaisonFilesView();
    }
    return h('div', {}, [
      h('div', { class: 'muted' }, ['远程管理套件（受 Casbin RBAC 鉴权）：文件管理 / 数据库 / LLM 对话 / 策略管理 / Web SSH。']),
      remoteMsgEl(), body
    ]);
  }
  function liaisonFilesView() {
    var rows = state.liaisonFiles.map(function (f) {
      var name = f.dir ? f.name + '/' : f.name;
      var onclick = f.dir
        ? function () { state.liaisonFilesPath = f.path; state.liaisonFiles = []; state.remoteLoaded.files = false; loadLiaison().then(render); }
        : function () { state.liaisonFileContent = ''; api('/api/liaison/files/content?path=' + encodeURIComponent(f.path)).then(function (d) { state.liaisonFileContent = d || ''; render(); }).catch(function (e) { remoteSetMsg(false, e.message); render(); }); };
      return h('tr', {}, [h('td', {}, [h('a', { href: '#', onclick: onclick }, [name])]), h('td', {}, [f.dir ? '目录' : String(f.size)]), h('td', {}, [f.mtime ? new Date(f.mtime * 1000).toLocaleString() : ''])]);
    });
    var up = h('button', { class: 'btn sm', onclick: function () {
      var p = state.liaisonFilesPath.replace(/\/+$/, '').split('/');
      p.pop();
      state.liaisonFilesPath = p.join('/') || '/';
      state.liaisonFiles = []; state.remoteLoaded.files = false; loadLiaison().then(render);
    } }, ['上一级']);
    var pathEl = h('input', { value: state.liaisonFilesPath, onchange: function () { state.liaisonFilesPath = pathEl.value || '/'; state.liaisonFiles = []; state.remoteLoaded.files = false; loadLiaison().then(render); } }, []);
    return h('div', { class: 'card' }, [
      h('div', { class: 'card-head' }, [h('h3', {}, ['文件管理'])]),
      h('div', { class: 'form-row2' }, [up, pathEl]),
      h('div', { class: 'table-wrap' }, [h('table', {}, [h('thead', {}, [h('tr', {}, [h('th', {}, ['名称']), h('th', {}, ['大小']), h('th', {}, ['修改时间'])])]), h('tbody', {}, rows)])]),
      state.liaisonFileContent !== '' ? h('pre', { class: 'file-content' }, [state.liaisonFileContent]) : null
    ]);
  }
  function liaisonDBView() {
    var rows = state.liaisonDB.map(function (c) {
      return h('tr', {}, [h('td', {}, [c.name || '']), h('td', {}, [c.driver || '']), h('td', {}, [c.dsn || '']), h('td', {}, [h('button', { class: 'btn sm danger', onclick: function () { liaisonDBDel(c.id); } }, ['删除'])])]);
    });
    var name = h('input', { placeholder: '名称' }, []);
    var driver = h('select', {}, [h('option', { value: 'postgres' }, ['postgres']), h('option', { value: 'sqlite' }, ['sqlite'])]);
    var dsn = h('input', { placeholder: 'DSN，如 file:/path/to.db 或 postgresql://...' }, []);
    var add = h('button', { class: 'btn primary', onclick: function () {
      api('/api/liaison/db/connections', { method: 'POST', body: JSON.stringify({ name: name.value, driver: driver.value, dsn: dsn.value }) })
        .then(function () { remoteSetMsg(true, '连接已添加'); state.liaisonDB = []; state.remoteLoaded.db = false; return loadLiaison().then(render); })
        .catch(function (e) { remoteSetMsg(false, e.message); render(); });
    } }, ['添加连接']);
    var connSel = h('select', {}, state.liaisonDB.map(function (c) { return h('option', { value: c.id }, [c.name]); }));
    var sql = h('textarea', { placeholder: 'SELECT ...' }, []);
    var run = h('button', { class: 'btn primary', onclick: function () {
      api('/api/liaison/db/query', { method: 'POST', body: JSON.stringify({ connection_id: Number(connSel.value), sql: sql.value }) })
        .then(function (d) { state.liaisonDBResult = d; remoteSetMsg(true, '查询完成（' + d.elapsed_ms + 'ms）'); render(); })
        .catch(function (e) { remoteSetMsg(false, e.message); render(); });
    } }, ['执行查询']);
    var resultEl = null;
    if (state.liaisonDBResult) {
      var r = state.liaisonDBResult;
      resultEl = h('div', { class: 'table-wrap' }, [h('table', {}, [
        h('thead', {}, [h('tr', {}, (r.columns || []).map(function (c) { return h('th', {}, [c]); }))]),
        h('tbody', {}, (r.rows || []).map(function (row) { return h('tr', {}, row.map(function (v) { return h('td', {}, [v == null ? 'NULL' : String(v)]); })); }))
      ])]);
    }
    return h('div', { class: 'card' }, [
      h('div', { class: 'card-head' }, [h('h3', {}, ['Web 数据库'])]),
      h('div', { class: 'table-wrap' }, [h('table', {}, [h('thead', {}, [h('tr', {}, [h('th', {}, ['名称']), h('th', {}, ['驱动']), h('th', {}, ['DSN']), h('th', {}, ['操作'])])]), h('tbody', {}, rows)])]),
      field('名称', name), field('驱动', driver), field('DSN', dsn), add,
      field('连接', connSel), field('SQL', sql), run, resultEl
    ]);
  }
  function liaisonDBDel(id) {
    if (!window.confirm('确认删除该连接？')) return;
    api('/api/liaison/db/connections/' + id, { method: 'DELETE' }).then(function () { state.liaisonDB = []; state.remoteLoaded.db = false; return loadLiaison().then(render); }).catch(function (e) { remoteSetMsg(false, e.message); render(); });
  }
  function liaisonLLMView() {
    var chat = h('textarea', { placeholder: '输入消息…' }, []);
    var send = h('button', { class: 'btn primary', onclick: function () {
      api('/api/liaison/llm/chat', { method: 'POST', body: JSON.stringify({ messages: [{ role: 'user', content: chat.value }] }) })
        .then(function (d) { state.liaisonLLMReply = d.content || ''; render(); })
        .catch(function (e) { remoteSetMsg(false, e.message); render(); });
    } }, ['发送']);
    var info = state.liaisonLLMModels ? ('模型：' + (state.liaisonLLMModels.model || '未配置')) : '';
    return h('div', { class: 'card' }, [
      h('div', { class: 'card-head' }, [h('h3', {}, ['LLM 网关'])]),
      h('div', { class: 'muted' }, [info]),
      field('消息', chat), send,
      state.liaisonLLMReply ? h('pre', { class: 'file-content' }, [state.liaisonLLMReply]) : null
    ]);
  }
  function liaisonPoliciesView() {
    var rows = state.liaisonPolicies.map(function (p) {
      return h('tr', {}, [h('td', {}, [p[0] || '']), h('td', {}, [p[1] || '']), h('td', {}, [p[2] || ''])]);
    });
    var sub = h('input', { placeholder: 'subject（角色，如 admin / user）' }, []);
    var obj = h('input', { placeholder: 'object（如 ssh:10.0.0.1 / files:* / *）' }, []);
    var act = h('input', { placeholder: 'action（如 connect / read / write / exec / *）' }, []);
    var add = h('button', { class: 'btn primary', onclick: function () {
      api('/api/liaison/policies', { method: 'POST', body: JSON.stringify({ sub: sub.value, obj: obj.value, act: act.value }) })
        .then(function () { remoteSetMsg(true, '策略已添加'); state.liaisonPolicies = []; state.remoteLoaded.policies = false; return loadLiaison().then(render); })
        .catch(function (e) { remoteSetMsg(false, e.message); render(); });
    } }, ['添加策略']);
    return h('div', { class: 'card' }, [
      h('div', { class: 'card-head' }, [h('h3', {}, ['Casbin 策略管理'])]),
      h('div', { class: 'muted' }, ['默认 superadmin/admin 拥有全部权限；可为普通用户显式授权。']),
      h('div', { class: 'table-wrap' }, [h('table', {}, [h('thead', {}, [h('tr', {}, [h('th', {}, ['subject']), h('th', {}, ['object']), h('th', {}, ['action'])])]), h('tbody', {}, rows)])]),
      field('subject', sub), field('object', obj), field('action', act), add
    ]);
  }
  function liaisonSSHView() {
    var host = h('input', { placeholder: '目标主机（overlay IP，如 10.77.0.2）' }, []);
    var port = h('input', { placeholder: '端口', type: 'number', value: 22 }, []);
    var user = h('input', { placeholder: '用户名', value: 'root' }, []);
    var pass = h('input', { placeholder: '密码', type: 'password' }, []);
    var termHost = h('div', { class: 'ssh-xterm', id: 'ssh-xterm' }, [h('div', { class: 'muted' }, ['（输入主机与凭据后点「连接」）'])]);
    var connectBtn = h('button', { class: 'btn primary', onclick: function () { liaisonSSHConnect(host, port, user, pass, termHost); } }, ['连接']);
    return h('div', { class: 'card' }, [
      h('div', { class: 'card-head' }, [h('h3', {}, ['Web SSH'])]),
      h('div', { class: 'form-row2' }, [host, port, user, pass, connectBtn]),
      termHost
    ]);
  }
  function liaisonSSHConnect(host, port, user, pass, termHost) {
    if (typeof Terminal === 'undefined' || typeof FitAddon === 'undefined') {
      termHost.replaceChildren(h('div', { class: 'err field' }, ['xterm.js 未加载']));
      return;
    }
    var term = new Terminal({ cursorBlink: true, fontSize: 13, theme: { background: '#171a1f' }, scrollback: 2000 });
    var fit = new FitAddon.FitAddon();
    term.loadAddon(fit);
    termHost.replaceChildren();
    term.open(termHost);
    fit.fit();
    term.focus();
    term.writeln('\x1b[33m正在连接 ' + host.value + ':' + port.value + '...\x1b[0m');
    var ws;
    try {
      var proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
      ws = new WebSocket(proto + '//' + location.host + '/api/liaison/ws');
    } catch (e) { term.writeln('\x1b[31m[连接失败] ' + e.message + '\x1b[0m'); return; }
    ws.binaryType = 'arraybuffer';
    ws.onopen = function () {
      ws.send(JSON.stringify({ kind: 'ssh', host: host.value, port: Number(port.value || 22), user: user.value, password: pass.value, cols: term.cols, rows: term.rows }));
    };
    ws.onmessage = function (ev) {
      if (typeof ev.data === 'string') {
        try {
          var m = JSON.parse(ev.data);
          if (m.type === 'ready') { term.writeln('\x1b[32m[已连接]\x1b[0m'); term.focus(); }
          else if (m.type === 'error') { term.writeln('\x1b[31m[错误] ' + m.message + '\x1b[0m'); }
        } catch (e) { term.write(ev.data); }
      } else {
        term.write(new Uint8Array(ev.data));
      }
    };
    ws.onclose = function () { term.writeln('\x1b[33m[连接已关闭]\x1b[0m'); };
    ws.onerror = function () { term.writeln('\x1b[31m[连接错误]\x1b[0m'); };
    term.onData(function (data) {
      if (ws && ws.readyState === 1) ws.send(data);
    });
  }
  function liaisonVNCView() {
    var host = h('input', { placeholder: '目标主机（overlay IP，如 10.77.0.2）' }, []);
    var port = h('input', { placeholder: '端口', type: 'number', value: 5900 }, []);
    var pass = h('input', { placeholder: '密码（可选）', type: 'password' }, []);
    var wrap = h('div', { class: 'vnc-wrap', id: 'vnc-wrap' }, [h('div', { class: 'muted' }, ['（输入主机信息后点「连接」，需目标运行 VNC 服务）'])]);
    var connectBtn = h('button', { class: 'btn primary', onclick: function () { liaisonVNCConnect(host, port, pass, wrap); } }, ['连接']);
    return h('div', { class: 'card' }, [
      h('div', { class: 'card-head' }, [h('h3', {}, ['远程桌面（VNC）'])]),
      h('div', { class: 'form-row2' }, [host, port, pass, connectBtn]),
      wrap
    ]);
  }
  function liaisonVNCConnect(host, port, pass, wrap) {
    var ws;
    try {
      var proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
      ws = new WebSocket(proto + '//' + location.host + '/api/liaison/ws');
    } catch (e) { wrap.replaceChildren(h('div', { class: 'err field' }, ['[连接失败] ' + e.message])); return; }
    ws.binaryType = 'arraybuffer';
    var canvas = null, ctx = null, imgData = null, W = 0, H = 0;
    function setMsg(t) { wrap.replaceChildren(h('div', { class: 'muted' }, [t])); }
    ws.onopen = function () {
      ws.send(JSON.stringify({ kind: 'desktop', sub: 'vnc', host: host.value, port: Number(port.value || 5900), password: pass.value }));
    };
    ws.onmessage = function (ev) {
      if (typeof ev.data === 'string') {
        try {
          var m = JSON.parse(ev.data);
          if (m.type === 'ready') {
            W = m.width; H = m.height;
            canvas = document.createElement('canvas');
            canvas.width = W; canvas.height = H;
            canvas.style.cssText = 'max-width:100%;background:#000;border-radius:6px;';
            ctx = canvas.getContext('2d');
            imgData = ctx.createImageData(W, H);
            wrap.replaceChildren(canvas);
            attachVNCInput(canvas, ws, W, H);
          } else if (m.type === 'error') { setMsg('[错误] ' + m.message); }
        } catch (e2) {}
      } else if (canvas) {
        var dv = new DataView(ev.data);
        var x = dv.getUint16(0), y = dv.getUint16(2), w = dv.getUint16(4), h = dv.getUint16(6);
        var px = new Uint8ClampedArray(ev.data, 8);
        for (var yy = 0; yy < h; yy++) {
          var srcOff = yy * w * 4;
          var dstOff = ((y + yy) * W + x) * 4;
          imgData.data.set(px.subarray(srcOff, srcOff + w * 4), dstOff);
        }
        ctx.putImageData(imgData, 0, 0);
      }
    };
    ws.onclose = function () { setMsg('[连接已关闭]'); };
    ws.onerror = function () { setMsg('[连接错误]'); };
  }
  function attachVNCInput(canvas, ws, W, H) {
    var buttonMask = 0;
    function sendPointer(ev, extra) {
      var scaleX = W / (canvas.clientWidth || 1), scaleY = H / (canvas.clientHeight || 1);
      var rect = canvas.getBoundingClientRect();
      var x = Math.round((ev.clientX - rect.left) * scaleX);
      var y = Math.round((ev.clientY - rect.top) * scaleY);
      if (x < 0) x = 0; if (y < 0) y = 0;
      if (x >= W) x = W - 1; if (y >= H) y = H - 1;
      var buf = new ArrayBuffer(7);
      var dv = new DataView(buf);
      dv.setUint8(0, 2);
      dv.setUint8(1, buttonMask | (extra || 0));
      dv.setUint16(2, x);
      dv.setUint16(4, y);
      ws.send(buf);
    }
    canvas.addEventListener('mousemove', function (e) { sendPointer(e, 0); });
    canvas.addEventListener('mousedown', function (e) { if (e.button === 0) buttonMask = 1; sendPointer(e, 0); });
    canvas.addEventListener('mouseup', function (e) { if (e.button === 0) buttonMask = 0; sendPointer(e, 0); });
    canvas.addEventListener('contextmenu', function (e) { e.preventDefault(); });
    canvas.tabIndex = 0;
    canvas.focus();
    canvas.addEventListener('keydown', function (e) { e.preventDefault(); vncKey(ws, e.keyCode, 1); });
    canvas.addEventListener('keyup', function (e) { vncKey(ws, e.keyCode, 0); });
  }
  function vncKey(ws, keyCode, down) {
    var buf = new ArrayBuffer(8);
    var dv = new DataView(buf);
    dv.setUint8(0, 1);
    dv.setUint8(1, down);
    dv.setUint32(4, keyCode);
    ws.send(buf);
  }
  function liaisonRDPView() {
    var host = h('input', { placeholder: '目标主机（overlay IP 或 Windows IP，如 10.77.0.2）' }, []);
    var port = h('input', { placeholder: '端口', type: 'number', value: 3389 }, []);
    var user = h('input', { placeholder: '用户名' }, []);
    var pass = h('input', { placeholder: '密码', type: 'password' }, []);
    var wrap = h('div', { class: 'vnc-wrap', id: 'rdp-wrap' }, [h('div', { class: 'muted' }, ['（输入 Windows 主机信息后点「连接」）'])]);
    var connectBtn = h('button', { class: 'btn primary', onclick: function () { liaisonRDPConnect(host, port, user, pass, wrap); } }, ['连接']);
    return h('div', { class: 'card' }, [
      h('div', { class: 'card-head' }, [h('h3', {}, ['远程桌面（RDP）'])]),
      h('div', { class: 'form-row2' }, [host, port, user, pass, connectBtn]),
      wrap
    ]);
  }
  function liaisonRDPConnect(host, port, user, pass, wrap) {
    var ws;
    try {
      var proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
      ws = new WebSocket(proto + '//' + location.host + '/api/liaison/ws');
    } catch (e) { wrap.replaceChildren(h('div', { class: 'err field' }, ['[连接失败] ' + e.message])); return; }
    ws.binaryType = 'arraybuffer';
    var canvas = null, ctx = null, imgData = null, W = 0, H = 0;
    function setMsg(t) { wrap.replaceChildren(h('div', { class: 'muted' }, [t])); }
    ws.onopen = function () {
      ws.send(JSON.stringify({ kind: 'desktop', sub: 'rdp', host: host.value, port: Number(port.value || 3389), user: user.value, password: pass.value }));
    };
    ws.onmessage = function (ev) {
      if (typeof ev.data === 'string') {
        try {
          var m = JSON.parse(ev.data);
          if (m.type === 'ready') {
            W = m.width; H = m.height;
            canvas = document.createElement('canvas');
            canvas.width = W; canvas.height = H;
            canvas.style.cssText = 'max-width:100%;background:#000;border-radius:6px;';
            ctx = canvas.getContext('2d');
            imgData = ctx.createImageData(W, H);
            wrap.replaceChildren(canvas);
            attachVNCInput(canvas, ws, W, H);
          } else if (m.type === 'error') { setMsg('[错误] ' + m.message); }
        } catch (e2) {}
      } else if (canvas) {
        var dv = new DataView(ev.data);
        var x = dv.getUint16(0), y = dv.getUint16(2), w = dv.getUint16(4), h = dv.getUint16(6);
        var px = new Uint8ClampedArray(ev.data, 8);
        for (var yy = 0; yy < h; yy++) {
          var srcOff = yy * w * 4;
          var dstOff = ((y + yy) * W + x) * 4;
          imgData.data.set(px.subarray(srcOff, srcOff + w * 4), dstOff);
        }
        ctx.putImageData(imgData, 0, 0);
      }
    };
    ws.onclose = function () { setMsg('[连接已关闭]'); };
    ws.onerror = function () { setMsg('[连接错误]'); };
  }
'''

# 远程访问 + 远程管理菜单（插入到「隧道」菜单后）
REMOTE_NAV = r'''
        navDropdown('远程管理', [
          { label: '文件管理', active: state.view === 'liaison' && state.liaisonSection === 'files', onclick: function () { state.liaisonSection = 'files'; switchView('liaison'); } },
          { label: '数据库', active: state.view === 'liaison' && state.liaisonSection === 'db', onclick: function () { state.liaisonSection = 'db'; switchView('liaison'); } },
          { label: 'LLM 对话', active: state.view === 'liaison' && state.liaisonSection === 'llm', onclick: function () { state.liaisonSection = 'llm'; switchView('liaison'); } },
          { label: '策略管理', active: state.view === 'liaison' && state.liaisonSection === 'policies', onclick: function () { state.liaisonSection = 'policies'; switchView('liaison'); } },
          { label: 'Web SSH', active: state.view === 'liaison' && state.liaisonSection === 'ssh', onclick: function () { state.liaisonSection = 'ssh'; switchView('liaison'); } },
          { label: '远程桌面（VNC）', active: state.view === 'liaison' && state.liaisonSection === 'vnc', onclick: function () { state.liaisonSection = 'vnc'; switchView('liaison'); } },
          { label: '远程桌面（RDP）', active: state.view === 'liaison' && state.liaisonSection === 'rdp', onclick: function () { state.liaisonSection = 'rdp'; switchView('liaison'); } }
        ], state.view === 'liaison'),
'''

REMOTE_CASE = r"""      case 'ddnsto': body = ddnstoView(); break;
      case 'liaison': body = liaisonView(); break;
"""

REMOTE_SWITCHVIEW = r"""    else if (v === 'ddnsto') loadDDNSTO().then(render);
    else if (v === 'liaison') render();
"""


TUNNEL_NAV = r'''
        navDropdown('隧道', [
          { label: '客户端', active: state.view === 'tunnel' && state.tunnelSection === 'clients', onclick: function () { state.tunnelSection = 'clients'; switchView('tunnel'); } },
          { label: '网络', active: state.view === 'tunnel' && state.tunnelSection === 'networks', onclick: function () { state.tunnelSection = 'networks'; switchView('tunnel'); } },
          { label: '服务端节点', active: state.view === 'tunnel' && state.tunnelSection === 'serverNodes', onclick: function () { state.tunnelSection = 'serverNodes'; switchView('tunnel'); } },
          { label: '路由', active: state.view === 'tunnel' && state.tunnelSection === 'routes', onclick: function () { state.tunnelSection = 'routes'; switchView('tunnel'); } },
          { label: '边缘服务', active: state.view === 'tunnel' && state.tunnelSection === 'edgeServices', onclick: function () { state.tunnelSection = 'edgeServices'; switchView('tunnel'); } },
          { label: '远程访问（DDNSTO）', active: state.view === 'ddnsto', onclick: function () { switchView('ddnsto'); } }
        ], state.view === 'tunnel' || state.view === 'ddnsto'),
'''

# switch 分发 case
TUNNEL_CASE = r"""      case 'tunnel': body = tunnelView(); break;
"""

# switchView 分支：否则 switchView('tunnel') 会落到 else loadHosts() 被改回 hosts
TUNNEL_SWITCHVIEW = r"""    else if (v === 'tunnel') render();
"""


# 密钥显示：列表接口默认脱敏，superadmin 显式点击后调用 reveal 接口取完整值。
SETTINGS_JS = r"""
  // ===== 密钥显示（superadmin 显式操作，服务端写审计）=====
  state.revealedSecrets = state.revealedSecrets || {};
  state.secretValues = state.secretValues || {};
  var _origSettingsView = settingsView;
  settingsView = function () {
    var node = _origSettingsView();
    if (!node || !node.querySelectorAll) return node;
    var wraps = node.querySelectorAll('[data-setting-key]');
    for (var i = 0; i < wraps.length; i++) {
      var wrap = wraps[i];
      var inp = wrap.querySelector('input[type="password"], input[data-secret-revealed]');
      if (!inp) continue;
      var key = wrap.getAttribute('data-setting-key');
      if (state.revealedSecrets[key] && state.secretValues[key] !== undefined) {
        inp.type = 'text';
        inp.setAttribute('data-secret-revealed', '1');
        inp.value = state.secretValues[key];
      }
      var btn = h('button', { type: 'button', class: 'btn ghost small' },
        [state.revealedSecrets[key] ? '隐藏' : '显示完整值']);
      btn.onclick = (function (key, inp, btn) {
        return function () {
          if (state.revealedSecrets[key]) {
            delete state.revealedSecrets[key];
            delete state.secretValues[key];
            inp.type = 'password';
            inp.removeAttribute('data-secret-revealed');
            var cur = (state.settings || []).filter(function (s) { return s.key === key; })[0];
            inp.value = cur ? (cur.value || '') : '';
            btn.textContent = '显示完整值';
            return;
          }
          api('/api/settings/reveal/' + encodeURIComponent(key)).then(function (d) {
            state.revealedSecrets[key] = true;
            state.secretValues[key] = (d && d.value) || '';
            inp.type = 'text';
            inp.setAttribute('data-secret-revealed', '1');
            inp.value = state.secretValues[key];
            btn.textContent = '隐藏';
          }).catch(function (e) { alert('读取完整密钥失败: ' + e.message); });
        };
      })(key, inp, btn);
      wrap.appendChild(h('div', { class: 'toolbar', style: 'margin-top:6px' }, [btn]));
    }
    // 恢复为配置文件值时清掉明文缓存，避免继续展示旧密钥。
    node.addEventListener('click', function (ev) {
      var t = ev.target;
      if (!t || !t.getAttribute) return;
      var k = t.getAttribute('data-key');
      if (k && String(t.className || '').indexOf('setting-reset') >= 0) {
        delete state.revealedSecrets[k];
        delete state.secretValues[k];
      }
    }, true);
    return node;
  };
"""

def inject(app_js):
    """在锚点注入隧道 + 远程访问 + 远程管理视图代码，返回注入后的内容。"""
    # 1. state 末尾注入 state 字段 + 视图函数
    anchor_state = "navOpen: null };"
    if anchor_state not in app_js:
        raise RuntimeError("未找到 state 末尾锚点: " + anchor_state)
    app_js = app_js.replace(anchor_state, anchor_state + "\n" + TUNNEL_JS + REMOTE_JS + SETTINGS_JS, 1)

    # 2. 导航菜单注入（隧道下拉 + 远程访问 + 远程管理下拉）
    anchor_nav = "navDropdown('帮助', helpItems, false)"
    if anchor_nav not in app_js:
        raise RuntimeError("未找到导航锚点: " + anchor_nav)
    nav_block = TUNNEL_NAV.rstrip("\n") + "\n" + REMOTE_NAV.rstrip("\n") + "\n        " + anchor_nav
    app_js = app_js.replace(anchor_nav, nav_block, 1)

    # 3. switch case 注入
    anchor_case = "default: body = hostsView();"
    if anchor_case not in app_js:
        raise RuntimeError("未找到 switch 分发锚点: " + anchor_case)
    app_js = app_js.replace(anchor_case, TUNNEL_CASE.rstrip("\n") + "\n" + REMOTE_CASE.rstrip("\n") + "\n      " + anchor_case, 1)

    # 4. switchView 分支注入
    anchor_switchview = "else if (v === 'password') render();"
    if anchor_switchview not in app_js:
        raise RuntimeError("未找到 switchView 锚点: " + anchor_switchview)
    app_js = app_js.replace(anchor_switchview, anchor_switchview + "\n" + TUNNEL_SWITCHVIEW.rstrip("\n") + "\n" + REMOTE_SWITCHVIEW.rstrip("\n"), 1)

    # 5. KNOWN_VIEWS 加入 tunnel/ddnsto/liaison（hash 路由恢复）
    anchor_known = "ai: 1, assistant: 1"
    if anchor_known not in app_js:
        raise RuntimeError("未找到 KNOWN_VIEWS 锚点: " + anchor_known)
    app_js = app_js.replace(anchor_known, anchor_known + ",\n    tunnel: 1, ddnsto: 1, liaison: 1", 1)

    return app_js


def main():
    os.makedirs(DST, exist_ok=True)

    # 1. 复制 index.html / assets / 图标
    for name in ["index.html", "favicon.ico", "apple-touch-icon.png"]:
        s = os.path.join(SRC, name)
        if os.path.exists(s):
            shutil.copy2(s, os.path.join(DST, name))
    # 注入 xterm.js 依赖（Web SSH 终端）
    index_path = os.path.join(DST, "index.html")
    with open(index_path, "r", encoding="utf-8") as f:
        idx = f.read()
    idx = idx.replace(
        '<link rel="stylesheet" href="/assets/app.css" />',
        '<link rel="stylesheet" href="/assets/app.css" />\n  <link rel="stylesheet" href="/assets/vendor/xterm.css" />',
    )
    idx = idx.replace(
        '<script src="/assets/app.js"></script>',
        '<script src="/assets/vendor/xterm.js"></script>\n  <script src="/assets/vendor/addon-fit.js"></script>\n  <script src="/assets/app.js"></script>',
    )
    with open(index_path, "w", encoding="utf-8") as f:
        f.write(idx)

    assets_dst = os.path.join(DST, "assets")
    os.makedirs(assets_dst, exist_ok=True)
    shutil.copy2(os.path.join(SRC, "assets", "app.css"), os.path.join(assets_dst, "app.css"))
    # 追加 iflygo 注入视图的补充样式（表单行/终端）
    with open(os.path.join(assets_dst, "app.css"), "a", encoding="utf-8") as f:
        f.write(EXTRA_CSS)
    # 复制 iflygo 自有 vendor 资源（xterm.js 等）
    vendor_dst = os.path.join(assets_dst, "vendor")
    os.makedirs(vendor_dst, exist_ok=True)
    vendor_src = os.path.join(ROOT, "web", "vendor")
    if os.path.isdir(vendor_src):
        for vf in os.listdir(vendor_src):
            shutil.copy2(os.path.join(vendor_src, vf), os.path.join(vendor_dst, vf))

    # 2. 注入隧道视图
    app_js_path = os.path.join(SRC, "assets", "app.js")
    with open(app_js_path, "r", encoding="utf-8") as f:
        app_js = f.read()
    app_js = inject(app_js)
    with open(os.path.join(assets_dst, "app.js"), "w", encoding="utf-8") as f:
        f.write(app_js)

    # 3. 复制 quickstart.json（upgrade_mgr 生成的页面 JSON）
    qs_src = os.path.join(ROOT, "third_party", "upgrade_mgr", "web", "quickstart.json")
    if os.path.exists(qs_src):
        shutil.copy2(qs_src, os.path.join(ROOT, "web", "quickstart.json"))

    print("✅ 前端合并完成：upgrade_mgr 前端 + 隧道管理视图 → web/dist")
    print("   请运行: node --check web/dist/assets/app.js 验证 JS 语法")


if __name__ == "__main__":
    main()
