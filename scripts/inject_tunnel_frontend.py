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
    var host = h('input', { placeholder: '目标主机（overlay IP）', value: '10.66.0.1' }, []);
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
    var host = h('input', { placeholder: '目标主机（overlay IP）', value: '10.66.0.1' }, []);
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
'''

# 远程访问 + 远程管理菜单（插入到「隧道」菜单后）
REMOTE_NAV = r'''
        navDropdown('远程管理', [
          { label: '文件管理', active: state.view === 'liaison' && state.liaisonSection === 'files', onclick: function () { state.liaisonSection = 'files'; switchView('liaison'); } },
          { label: '数据库', active: state.view === 'liaison' && state.liaisonSection === 'db', onclick: function () { state.liaisonSection = 'db'; switchView('liaison'); } },
          { label: 'LLM 对话', active: state.view === 'liaison' && state.liaisonSection === 'llm', onclick: function () { state.liaisonSection = 'llm'; switchView('liaison'); } },
          { label: '策略管理', active: state.view === 'liaison' && state.liaisonSection === 'policies', onclick: function () { state.liaisonSection = 'policies'; switchView('liaison'); } },
          { label: 'Web SSH', active: state.view === 'liaison' && state.liaisonSection === 'ssh', onclick: function () { state.liaisonSection = 'ssh'; switchView('liaison'); } },
          { label: '远程桌面（VNC）', active: state.view === 'liaison' && state.liaisonSection === 'vnc', onclick: function () { state.liaisonSection = 'vnc'; switchView('liaison'); } }
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


def inject(app_js):
    """在锚点注入隧道 + 远程访问 + 远程管理视图代码，返回注入后的内容。"""
    # 1. state 末尾注入 state 字段 + 视图函数
    anchor_state = "navOpen: null };"
    if anchor_state not in app_js:
        raise RuntimeError("未找到 state 末尾锚点: " + anchor_state)
    app_js = app_js.replace(anchor_state, anchor_state + "\n" + TUNNEL_JS + REMOTE_JS, 1)

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
