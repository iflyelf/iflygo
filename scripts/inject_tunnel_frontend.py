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

  function tunnelClientsView() {
    var rows = state.tunnelClients.map(function (c) {
      return h('tr', {}, [
        h('td', {}, [c.name || '']),
        h('td', {}, [c.ipv4 || '—']),
        h('td', {}, [c.ipv6 || '—']),
        h('td', {}, [c.status || '—']),
        h('td', {}, [
          h('button', { class: 'btn sm', onclick: function () { tunnelGetToken(c.id, c.name); } }, ['安装命令']),
          ' ',
          h('button', { class: 'btn sm', onclick: function () { tunnelRenewCert(c.id); } }, ['续期']),
          ' ',
          h('button', { class: 'btn sm', onclick: function () { tunnelRevokeCert(c.id); } }, ['吊销']),
          ' ',
          h('button', { class: 'btn sm danger', onclick: function () { tunnelDelClient(c.id); } }, ['删除'])
        ])
      ]);
    });
    var name = h('input', { placeholder: '名称' }, []);
    var net = tunnelNetSelect(state.tunnelNetworks[0] ? state.tunnelNetworks[0].id : 0);
    var ipv4 = h('input', { placeholder: 'IPv4（留空自动分配）' }, []);
    var ipv6 = h('input', { placeholder: 'IPv6（留空自动分配）' }, []);
    var create = h('button', { class: 'btn primary', onclick: function () {
      api('/api/clients', { method: 'POST', body: JSON.stringify({ network_id: Number(net.value), name: name.value, ipv4: ipv4.value, ipv6: ipv6.value }) })
        .then(function (d) {
          tunnelSetMsg(true, '已创建，安装命令：' + (d.install_command || ''));
          if (d.install_command && navigator.clipboard) { navigator.clipboard.writeText(d.install_command).catch(function () {}); }
          return tunnelReload();
        })
        .catch(function (e) { tunnelSetMsg(false, e.message); render(); });
    } }, ['创建客户端']);
    return h('div', { class: 'card' }, [
      h('div', { class: 'card-head' }, [h('h3', {}, ['客户端'])]),
      h('div', { class: 'table-wrap' }, [
        h('table', {}, [
          h('thead', {}, [h('tr', {}, [h('th', {}, ['名称']), h('th', {}, ['IPv4']), h('th', {}, ['IPv6']), h('th', {}, ['状态']), h('th', {}, ['操作'])])]),
          h('tbody', {}, rows)
        ])
      ]),
      field('名称', name),
      field('所属网络', net),
      field('IPv4', ipv4),
      field('IPv6', ipv6),
      create
    ]);
  }

  function tunnelNetworksView() {
    var rows = state.tunnelNetworks.map(function (n) {
      return h('tr', {}, [
        h('td', {}, [n.name || '']),
        h('td', {}, [n.cidr4 || '']),
        h('td', {}, [n.cidr6 || '—']),
        h('td', {}, [String(n.mtu || '')]),
        h('td', {}, [h('button', { class: 'btn sm danger', onclick: function () { tunnelDelNetwork(n.id); } }, ['删除'])])
      ]);
    });
    var name = h('input', { placeholder: '名称' }, []);
    var cidr4 = h('input', { placeholder: 'IPv4 CIDR，如 10.77.0.0/16' }, []);
    var cidr6 = h('input', { placeholder: 'IPv6 CIDR（可选）' }, []);
    var gw4 = h('input', { placeholder: 'IPv4 网关（可选）' }, []);
    var gw6 = h('input', { placeholder: 'IPv6 网关（可选）' }, []);
    var create = h('button', { class: 'btn primary', onclick: function () {
      api('/api/networks', { method: 'POST', body: JSON.stringify({ name: name.value, cidr4: cidr4.value, cidr6: cidr6.value, gateway4: gw4.value, gateway6: gw6.value }) })
        .then(function () { tunnelSetMsg(true, '网络已创建'); return tunnelReload(); })
        .catch(function (e) { tunnelSetMsg(false, e.message); render(); });
    } }, ['创建网络']);
    return h('div', { class: 'card' }, [
      h('div', { class: 'card-head' }, [h('h3', {}, ['网络'])]),
      h('div', { class: 'table-wrap' }, [
        h('table', {}, [
          h('thead', {}, [h('tr', {}, [h('th', {}, ['名称']), h('th', {}, ['IPv4']), h('th', {}, ['IPv6']), h('th', {}, ['MTU']), h('th', {}, ['操作'])])]),
          h('tbody', {}, rows)
        ])
      ]),
      field('名称', name), field('IPv4 CIDR', cidr4), field('IPv6 CIDR', cidr6),
      field('IPv4 网关', gw4), field('IPv6 网关', gw6),
      create
    ]);
  }

  function tunnelServerNodesView() {
    var rows = state.tunnelServerNodes.map(function (n) {
      return h('tr', {}, [
        h('td', {}, [n.name || '']),
        h('td', {}, [n.roles || '']),
        h('td', {}, [n.overlay_ip4 || '—']),
        h('td', {}, [(n.public_host || '') + (n.public_port ? ':' + n.public_port : '')]),
        h('td', {}, [h('button', { class: 'btn sm danger', onclick: function () { tunnelDelServerNode(n.id); } }, ['删除'])])
      ]);
    });
    var name = h('input', { placeholder: '名称' }, []);
    var net = tunnelNetSelect(state.tunnelNetworks[0] ? state.tunnelNetworks[0].id : 0);
    var roles = h('input', { placeholder: '角色，如 lighthouse,relay' }, []);
    var ip4 = h('input', { placeholder: 'overlay IPv4（留空自动）' }, []);
    var ip6 = h('input', { placeholder: 'overlay IPv6（留空自动）' }, []);
    var host = h('input', { placeholder: '公网 host' }, []);
    var port = h('input', { placeholder: '公网端口', type: 'number' }, []);
    var create = h('button', { class: 'btn primary', onclick: function () {
      api('/api/server-nodes', { method: 'POST', body: JSON.stringify({ network_id: Number(net.value), name: name.value, roles: roles.value, overlay_ip4: ip4.value, overlay_ip6: ip6.value, public_host: host.value, public_port: Number(port.value || 0) }) })
        .then(function () { tunnelSetMsg(true, '节点已创建'); return tunnelReload(); })
        .catch(function (e) { tunnelSetMsg(false, e.message); render(); });
    } }, ['创建节点']);
    return h('div', { class: 'card' }, [
      h('div', { class: 'card-head' }, [h('h3', {}, ['服务端节点（lighthouse/relay/edge/gateway）'])]),
      h('div', { class: 'table-wrap' }, [
        h('table', {}, [
          h('thead', {}, [h('tr', {}, [h('th', {}, ['名称']), h('th', {}, ['角色']), h('th', {}, ['overlay IP']), h('th', {}, ['公网入口']), h('th', {}, ['操作'])])]),
          h('tbody', {}, rows)
        ])
      ]),
      field('名称', name), field('所属网络', net), field('角色', roles),
      field('overlay IPv4', ip4), field('overlay IPv6', ip6),
      field('公网 host', host), field('公网端口', port),
      create
    ]);
  }

  function tunnelRoutesView() {
    var netSel = tunnelNetSelect(state.tunnelRouteNetwork, function () {
      state.tunnelRouteNetwork = Number(netSel.value);
      tunnelLoadRoutes().then(render);
    });
    var rows = state.tunnelRoutes.map(function (r) {
      var gws = (r.gateways || []).map(function (g) { return g.overlay_ip + '(w' + g.weight + ')'; }).join(', ');
      return h('tr', {}, [
        h('td', {}, [r.cidr || '']),
        h('td', {}, [r.mode || '']),
        h('td', {}, [gws]),
        h('td', {}, [h('button', { class: 'btn sm danger', onclick: function () { tunnelDelRoute(r.id); } }, ['删除'])])
      ]);
    });
    var cidr = h('input', { placeholder: 'CIDR，如 10.8.1.0/24' }, []);
    var gws = h('input', { placeholder: '网关 ip:weight,ip:weight，如 10.77.0.5:10,10.77.0.6:5' }, []);
    var mode = h('select', {}, [
      h('option', { value: 'active_active' }, ['active_active']),
      h('option', { value: 'active_passive' }, ['active_passive'])
    ]);
    var create = h('button', { class: 'btn primary', onclick: function () {
      var gateways = gws.value.split(',').map(function (s) {
        var parts = s.trim().split(':');
        return { overlay_ip: parts[0], weight: Number(parts[1] || 1), priority: 0 };
      }).filter(function (g) { return g.overlay_ip; });
      api('/api/networks/' + state.tunnelRouteNetwork + '/routes', { method: 'POST', body: JSON.stringify({ cidr: cidr.value, mtu: 1300, mode: mode.value, gateways: gateways }) })
        .then(function () { tunnelSetMsg(true, '路由已创建'); return tunnelLoadRoutes().then(render); })
        .catch(function (e) { tunnelSetMsg(false, e.message); render(); });
    } }, ['创建路由']);
    return h('div', { class: 'card' }, [
      h('div', { class: 'card-head' }, [h('h3', {}, ['不安全路由'])]),
      field('所属网络', netSel),
      h('div', { class: 'table-wrap' }, [
        h('table', {}, [
          h('thead', {}, [h('tr', {}, [h('th', {}, ['CIDR']), h('th', {}, ['模式']), h('th', {}, ['网关(权重)']), h('th', {}, ['操作'])])]),
          h('tbody', {}, rows)
        ])
      ]),
      field('CIDR', cidr), field('网关', gws), field('模式', mode),
      create
    ]);
  }

  function tunnelEdgeServicesView() {
    var rows = state.tunnelEdgeServices.map(function (s) {
      return h('tr', {}, [
        h('td', {}, [s.name || '']),
        h('td', {}, [s.protocol === 'tcp' ? ':' + (s.public_port || '') : (s.host || '')]),
        h('td', {}, [s.target || '']),
        h('td', {}, [s.protocol || '']),
        h('td', {}, [h('button', { class: 'btn sm danger', onclick: function () { tunnelDelEdgeService(s.id); } }, ['删除'])])
      ]);
    });
    var net = tunnelNetSelect(state.tunnelNetworks[0] ? state.tunnelNetworks[0].id : 0);
    var name = h('input', { placeholder: '名称' }, []);
    var proto = h('select', {}, [h('option', { value: 'http' }, ['http']), h('option', { value: 'tcp' }, ['tcp'])]);
    var host = h('input', { placeholder: '域名（http），如 nas.example.com' }, []);
    var pubPort = h('input', { placeholder: '公网端口（tcp）', type: 'number' }, []);
    var target = h('input', { placeholder: '目标，如 10.77.0.2:5000' }, []);
    var create = h('button', { class: 'btn primary', onclick: function () {
      api('/api/edge-services', { method: 'POST', body: JSON.stringify({ network_id: Number(net.value), name: name.value, host: host.value, target: target.value, protocol: proto.value, public_port: Number(pubPort.value || 0) }) })
        .then(function () { tunnelSetMsg(true, '服务已创建'); return tunnelReload(); })
        .catch(function (e) { tunnelSetMsg(false, e.message); render(); });
    } }, ['创建服务']);
    return h('div', { class: 'card' }, [
      h('div', { class: 'card-head' }, [h('h3', {}, ['边缘服务（自定义域名 / TCP 端口）'])]),
      h('div', { class: 'table-wrap' }, [
        h('table', {}, [
          h('thead', {}, [h('tr', {}, [h('th', {}, ['名称']), h('th', {}, ['入口']), h('th', {}, ['目标']), h('th', {}, ['协议']), h('th', {}, ['操作'])])]),
          h('tbody', {}, rows)
        ])
      ]),
      field('所属网络', net), field('名称', name), field('协议', proto),
      field('域名', host), field('公网端口', pubPort), field('目标', target),
      create
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
TUNNEL_NAV = r'''
        navDropdown('隧道', [
          { label: '客户端', active: state.view === 'tunnel' && state.tunnelSection === 'clients', onclick: function () { state.tunnelSection = 'clients'; switchView('tunnel'); } },
          { label: '网络', active: state.view === 'tunnel' && state.tunnelSection === 'networks', onclick: function () { state.tunnelSection = 'networks'; switchView('tunnel'); } },
          { label: '服务端节点', active: state.view === 'tunnel' && state.tunnelSection === 'serverNodes', onclick: function () { state.tunnelSection = 'serverNodes'; switchView('tunnel'); } },
          { label: '路由', active: state.view === 'tunnel' && state.tunnelSection === 'routes', onclick: function () { state.tunnelSection = 'routes'; switchView('tunnel'); } },
          { label: '边缘服务', active: state.view === 'tunnel' && state.tunnelSection === 'edgeServices', onclick: function () { state.tunnelSection = 'edgeServices'; switchView('tunnel'); } }
        ], state.view === 'tunnel'),
'''

# switch 分发 case
TUNNEL_CASE = r"""      case 'tunnel': body = tunnelView(); break;
"""

# switchView 分支：否则 switchView('tunnel') 会落到 else loadHosts() 被改回 hosts
TUNNEL_SWITCHVIEW = r"""    else if (v === 'tunnel') render();
"""


def inject(app_js):
    """在三处锚点注入隧道视图代码，返回注入后的内容。"""
    # 1. state 末尾注入 state 字段 + 视图函数
    anchor_state = "navOpen: null };"
    if anchor_state not in app_js:
        raise RuntimeError("未找到 state 末尾锚点: " + anchor_state)
    app_js = app_js.replace(anchor_state, anchor_state + "\n" + TUNNEL_JS, 1)

    # 2. 导航菜单注入
    anchor_nav = "navDropdown('帮助', helpItems, false)"
    if anchor_nav not in app_js:
        raise RuntimeError("未找到导航锚点: " + anchor_nav)
    app_js = app_js.replace(anchor_nav, TUNNEL_NAV.rstrip("\n") + "\n        " + anchor_nav, 1)

    # 3. switch case 注入
    anchor_case = "default: body = hostsView();"
    if anchor_case not in app_js:
        raise RuntimeError("未找到 switch 分发锚点: " + anchor_case)
    app_js = app_js.replace(anchor_case, TUNNEL_CASE.rstrip("\n") + "\n      " + anchor_case, 1)

    # 4. switchView 分支注入（否则 switchView('tunnel') 落到 else loadHosts()）
    anchor_switchview = "else if (v === 'password') render();"
    if anchor_switchview not in app_js:
        raise RuntimeError("未找到 switchView 锚点: " + anchor_switchview)
    app_js = app_js.replace(anchor_switchview, anchor_switchview + "\n" + TUNNEL_SWITCHVIEW.rstrip("\n"), 1)

    # 5. KNOWN_VIEWS 加入 tunnel（hash 路由恢复）
    anchor_known = "ai: 1, assistant: 1"
    if anchor_known not in app_js:
        raise RuntimeError("未找到 KNOWN_VIEWS 锚点: " + anchor_known)
    app_js = app_js.replace(anchor_known, anchor_known + ",\n    tunnel: 1", 1)

    return app_js


def main():
    os.makedirs(DST, exist_ok=True)

    # 1. 复制 index.html / assets / 图标
    for name in ["index.html", "favicon.ico", "apple-touch-icon.png"]:
        s = os.path.join(SRC, name)
        if os.path.exists(s):
            shutil.copy2(s, os.path.join(DST, name))
    assets_dst = os.path.join(DST, "assets")
    os.makedirs(assets_dst, exist_ok=True)
    shutil.copy2(os.path.join(SRC, "assets", "app.css"), os.path.join(assets_dst, "app.css"))

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
