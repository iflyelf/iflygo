(() => {
  'use strict';

  const state = {
    user: null,
    view: 'loading',
    section: 'clients',
    networks: [],
    clients: [],
    serverNodes: [],
    routes: [],
    edgeServices: [],
    routeNetwork: 0,
    msg: '',
    msgKind: 'err',
  };

  const app = document.getElementById('app');

  function h(tag, attrs, ...children) {
    const el = document.createElement(tag);
    if (attrs) {
      for (const [k, v] of Object.entries(attrs)) {
        if (v == null || v === false) continue;
        if (k === 'class') el.className = v;
        else if (k === 'onclick') el.onclick = v;
        else if (k === 'value') el.value = v;
        else if (k.startsWith('on')) el.addEventListener(k.slice(2), v);
        else el.setAttribute(k, v);
      }
    }
    for (const c of children.flat(Infinity)) {
      if (c == null) continue;
      el.append(c instanceof Node ? c : document.createTextNode(c));
    }
    return el;
  }

  async function api(path, opts = {}) {
    const o = Object.assign({ headers: { 'Content-Type': 'application/json' }, credentials: 'same-origin' }, opts);
    if (o.body && typeof o.body !== 'string') o.body = JSON.stringify(o.body);
    const res = await fetch(path, o);
    const data = await res.json().catch(() => ({}));
    if (res.status === 401 && state.user) { state.user = null; state.view = 'login'; }
    return { ok: res.ok, status: res.status, data };
  }

  function setMsg(text, kind) { state.msg = text || ''; state.msgKind = kind || 'err'; }

  async function loadAll() {
    const [n, c, s, e] = await Promise.all([
      api('/api/networks'), api('/api/clients'), api('/api/server-nodes'), api('/api/edge-services'),
    ]);
    state.networks = n.ok ? n.data : [];
    state.clients = c.ok ? c.data : [];
    state.serverNodes = s.ok ? s.data : [];
    state.edgeServices = e.ok ? e.data : [];
    await loadRoutes();
  }

  async function loadRoutes() {
    const nid = state.routeNetwork || (state.networks[0] && state.networks[0].id) || 0;
    state.routeNetwork = nid;
    if (!nid) { state.routes = []; return; }
    const r = await api('/api/networks/' + nid + '/routes');
    state.routes = r.ok ? r.data : [];
  }

  async function boot() {
    const u = await api('/api/auth/userinfo');
    if (u.ok && u.data && u.data.username) {
      state.user = u.data; state.view = 'dashboard'; await loadAll();
    } else { state.view = 'login'; }
    render();
  }

  function render() {
    app.replaceChildren();
    if (state.view === 'loading') { app.append(h('div', { class: 'card' }, '加载中…')); return; }
    if (state.view === 'login') return renderLogin();
    renderDashboard();
  }

  function renderLogin() {
    const user = h('input', { type: 'text', placeholder: '用户名', autocomplete: 'username' });
    const pass = h('input', { type: 'password', placeholder: '密码', autocomplete: 'current-password' });
    const btn = h('button', { class: 'btn primary', style: 'width:100%' }, '登录');
    const submit = async () => {
      const r = await api('/api/auth/login', { method: 'POST', body: { username: user.value, password: pass.value } });
      if (r.ok) { state.user = r.data; state.view = 'dashboard'; await loadAll(); render(); }
      else { setMsg(r.data.message || '登录失败'); render(); }
    };
    btn.onclick = submit;
    pass.onkeydown = (e) => { if (e.key === 'Enter') submit(); };
    app.append(h('div', { class: 'card', style: 'max-width:360px;margin:80px auto' },
      h('h1', {}, 'iFlyGo'), msgEl(), user, pass, btn));
  }

  function msgEl() {
    if (!state.msg) return null;
    return h('div', { class: 'msg ' + state.msgKind }, state.msg);
  }

  function renderDashboard() {
    const logout = h('button', { class: 'btn sm' }, '退出');
    logout.onclick = async () => { await api('/api/auth/logout', { method: 'POST' }); state.user = null; state.view = 'login'; render(); };

    const tabs = ['clients', 'networks', 'serverNodes', 'routes', 'edgeServices'].map((s) => {
      const labels = { clients: '客户端', networks: '网络', serverNodes: '节点', routes: '路由', edgeServices: '域名' };
      const cls = 'btn sm' + (state.section === s ? ' primary' : '');
      const b = h('button', { class: cls, onclick: () => { state.section = s; render(); } }, labels[s]);
      return b;
    });

    app.append(
      h('div', { class: 'topbar' }, h('span', { class: 'brand' }, 'iFlyGo 控制台'),
        h('div', { class: 'form-row' }, ...tabs, h('span', { class: 'muted' }, state.user.username + ' '), logout)),
      msgEl(),
      renderSection(),
    );
  }

  function renderSection() {
    switch (state.section) {
      case 'networks': return renderNetworks();
      case 'serverNodes': return renderServerNodes();
      case 'routes': return renderRoutes();
      case 'edgeServices': return renderEdgeServices();
      default: return renderClients();
    }
  }

  function netSelect(value, onchange) {
    const sel = h('select', {}, ...state.networks.map((n) => h('option', { value: n.id }, n.name + ' (' + n.cidr4 + ')')));
    sel.value = value;
    if (onchange) sel.onchange = onchange;
    return sel;
  }

  // ---------- 客户端 ----------
  function renderClients() {
    const rows = state.clients.map((c) => h('tr', {},
      h('td', {}, c.name), h('td', {}, c.ipv4 || '—'), h('td', {}, c.ipv6 || '—'),
      h('td', {}, h('span', { class: 'badge ' + (c.status === 'online' ? 'online' : 'offline') }, c.status)),
      h('td', {}, h('button', { class: 'btn sm', onclick: () => getToken(c.id, c.name) }, '安装命令'), ' ',
        h('button', { class: 'btn sm', onclick: () => renewCert(c.id) }, '续期'), ' ',
        h('button', { class: 'btn sm', onclick: () => revokeCert(c.id) }, '吊销'), ' ',
        h('button', { class: 'btn danger sm', onclick: () => delClient(c.id) }, '删除'))));

    const name = h('input', { placeholder: '名称' });
    const net = netSelect(state.networks[0] ? state.networks[0].id : 0);
    const ipv4 = h('input', { placeholder: 'IPv4 (留空自动)' });
    const ipv6 = h('input', { placeholder: 'IPv6 (留空自动)' });
    const create = h('button', { class: 'btn primary', style: 'width:100%' }, '创建客户端');
    create.onclick = async () => {
      const r = await api('/api/clients', { method: 'POST', body: { network_id: Number(net.value), name: name.value, ipv4: ipv4.value, ipv6: ipv6.value } });
      if (r.ok) { await loadAll(); setMsg('已创建，安装命令：' + r.data.install_command, 'ok'); try { navigator.clipboard.writeText(r.data.install_command); } catch (_) {} }
      else setMsg(r.data.message);
      render();
    };

    return h('div', { class: 'card' }, h('h2', {}, '客户端'),
      h('table', {}, h('thead', {}, h('tr', {}, h('th', {}, '名称'), h('th', {}, 'IPv4'), h('th', {}, 'IPv6'), h('th', {}, '状态'), h('th', {}))), h('tbody', {}, rows)),
      h('div', { class: 'muted', style: 'margin-bottom:8px' }, '新建客户端'), net, name, ipv4, ipv6, create);
  }

  // ---------- 网络 ----------
  function renderNetworks() {
    const rows = state.networks.map((n) => h('tr', {},
      h('td', {}, n.name), h('td', {}, n.cidr4), h('td', {}, n.cidr6 || '—'), h('td', {}, n.mtu),
      h('td', {}, h('button', { class: 'btn danger sm', onclick: () => delNetwork(n.id) }, '删除'))));

    const name = h('input', { placeholder: '名称' });
    const cidr4 = h('input', { placeholder: 'IPv4 CIDR, 如 10.77.0.0/16' });
    const cidr6 = h('input', { placeholder: 'IPv6 CIDR (可选)' });
    const gw4 = h('input', { placeholder: 'IPv4 网关 (可选)' });
    const gw6 = h('input', { placeholder: 'IPv6 网关 (可选)' });
    const create = h('button', { class: 'btn primary', style: 'width:100%' }, '创建网络');
    create.onclick = async () => {
      const r = await api('/api/networks', { method: 'POST', body: { name: name.value, cidr4: cidr4.value, cidr6: cidr6.value, gateway4: gw4.value, gateway6: gw6.value } });
      if (r.ok) { await loadAll(); setMsg('网络已创建', 'ok'); } else setMsg(r.data.message);
      render();
    };

    return h('div', { class: 'card' }, h('h2', {}, '网络'),
      h('table', {}, h('thead', {}, h('tr', {}, h('th', {}, '名称'), h('th', {}, 'IPv4'), h('th', {}, 'IPv6'), h('th', {}, 'MTU'), h('th', {}))), h('tbody', {}, rows)),
      h('div', { class: 'muted', style: 'margin-bottom:8px' }, '新建网络'), name, cidr4, cidr6, h('div', { class: 'form-row' }, gw4, gw6), create);
  }

  // ---------- 服务端节点 ----------
  function renderServerNodes() {
    const rows = state.serverNodes.map((n) => h('tr', {},
      h('td', {}, n.name), h('td', {}, n.roles), h('td', {}, n.overlay_ip4 || '—'), h('td', {}, n.public_host + (n.public_port ? ':' + n.public_port : '')),
      h('td', {}, h('button', { class: 'btn danger sm', onclick: () => delServerNode(n.id) }, '删除'))));

    const name = h('input', { placeholder: '名称' });
    const net = netSelect(state.networks[0] ? state.networks[0].id : 0);
    const roles = h('input', { placeholder: '角色, 如 lighthouse,relay' });
    const ip4 = h('input', { placeholder: 'overlay IPv4 (留空自动)' });
    const ip6 = h('input', { placeholder: 'overlay IPv6 (留空自动)' });
    const host = h('input', { placeholder: '公网 host' });
    const port = h('input', { placeholder: '公网端口', type: 'number' });
    const create = h('button', { class: 'btn primary', style: 'width:100%' }, '创建节点');
    create.onclick = async () => {
      const r = await api('/api/server-nodes', { method: 'POST', body: { network_id: Number(net.value), name: name.value, roles: roles.value, overlay_ip4: ip4.value, overlay_ip6: ip6.value, public_host: host.value, public_port: Number(port.value || 0) } });
      if (r.ok) { await loadAll(); setMsg('节点已创建', 'ok'); } else setMsg(r.data.message);
      render();
    };

    return h('div', { class: 'card' }, h('h2', {}, '服务端节点 (lighthouse/relay/edge/gateway)'),
      h('table', {}, h('thead', {}, h('tr', {}, h('th', {}, '名称'), h('th', {}, '角色'), h('th', {}, 'overlay IP'), h('th', {}, '公网入口'), h('th', {}))), h('tbody', {}, rows)),
      h('div', { class: 'muted', style: 'margin-bottom:8px' }, '新建节点'), net, name, roles, ip4, ip6, h('div', { class: 'form-row' }, host, port), create);
  }

  // ---------- 路由 ----------
  function renderRoutes() {
    const netSel = netSelect(state.routeNetwork, async () => { await loadRoutes(); render(); });
    const rows = state.routes.map((r) => {
      const gws = r.gateways.map((g) => g.overlay_ip + '(w' + g.weight + ')').join(', ');
      return h('tr', {}, h('td', {}, r.cidr), h('td', {}, r.mode), h('td', {}, gws),
        h('td', {}, h('button', { class: 'btn danger sm', onclick: () => delRoute(r.id) }, '删除')));
    });

    const cidr = h('input', { placeholder: 'CIDR, 如 10.8.1.0/24' });
    const gws = h('input', { placeholder: '网关 ip:weight,ip:weight, 如 10.77.0.5:10,10.77.0.6:5' });
    const mode = h('select', {}, h('option', { value: 'active_active' }, 'active_active'), h('option', { value: 'active_passive' }, 'active_passive'));
    const create = h('button', { class: 'btn primary', style: 'width:100%' }, '创建路由');
    create.onclick = async () => {
      const gateways = gws.value.split(',').map((s) => { const [ip, w] = s.trim().split(':'); return { overlay_ip: ip, weight: Number(w || 1), priority: 0 }; }).filter((g) => g.overlay_ip);
      const r = await api('/api/networks/' + state.routeNetwork + '/routes', { method: 'POST', body: { cidr: cidr.value, mtu: 1300, mode: mode.value, gateways } });
      if (r.ok) { await loadRoutes(); setMsg('路由已创建', 'ok'); } else setMsg(r.data.message);
      render();
    };

    return h('div', { class: 'card' }, h('h2', {}, '不安全路由'),
      netSel,
      h('table', {}, h('thead', {}, h('tr', {}, h('th', {}, 'CIDR'), h('th', {}, '模式'), h('th', {}, '网关(权重)'), h('th', {}))), h('tbody', {}, rows)),
      h('div', { class: 'muted', style: 'margin-bottom:8px' }, '新建路由'), cidr, gws, mode, create);
  }

  // ---------- 边缘服务 ----------
  function renderEdgeServices() {
    const rows = state.edgeServices.map((s) => h('tr', {},
      h('td', {}, s.name),
      h('td', {}, s.protocol === 'tcp' ? ':' + s.public_port : s.host),
      h('td', {}, s.target), h('td', {}, s.protocol),
      h('td', {}, h('button', { class: 'btn danger sm', onclick: () => delEdgeService(s.id) }, '删除'))));

    const net = netSelect(state.networks[0] ? state.networks[0].id : 0);
    const name = h('input', { placeholder: '名称' });
    const proto = h('select', {}, h('option', { value: 'http' }, 'http'), h('option', { value: 'tcp' }, 'tcp'));
    const host = h('input', { placeholder: '域名 (http), 如 nas.example.com' });
    const pubPort = h('input', { placeholder: '公网端口 (tcp)', type: 'number' });
    const target = h('input', { placeholder: '目标, 如 10.77.0.2:5000' });
    const create = h('button', { class: 'btn primary', style: 'width:100%' }, '创建域名服务');
    create.onclick = async () => {
      const r = await api('/api/edge-services', { method: 'POST', body: { network_id: Number(net.value), name: name.value, host: host.value, target: target.value, protocol: proto.value, public_port: Number(pubPort.value || 0) } });
      if (r.ok) { await loadAll(); setMsg('服务已创建', 'ok'); } else setMsg(r.data.message);
      render();
    };

    return h('div', { class: 'card' }, h('h2', {}, '边缘服务（自定义域名 / TCP 端口）'),
      h('table', {}, h('thead', {}, h('tr', {}, h('th', {}, '名称'), h('th', {}, '入口'), h('th', {}, '目标'), h('th', {}, '协议'), h('th', {}))), h('tbody', {}, rows)),
      h('div', { class: 'muted', style: 'margin-bottom:8px' }, '新建服务'), net, name, proto, host, pubPort, target, create);
  }

  // ---------- 操作 ----------
  async function getToken(id, name) {
    const r = await api('/api/clients/' + id + '/enroll-token', { method: 'POST' });
    if (r.ok) { setMsg(name + ' 安装命令：' + r.data.install_command, 'ok'); try { navigator.clipboard.writeText(r.data.install_command); } catch (_) {} } else setMsg(r.data.message);
    render();
  }
  async function renewCert(id) {
    if (!confirm('确认续期该客户端证书？')) return;
    const r = await api('/api/clients/' + id + '/cert/renew', { method: 'POST' });
    setMsg(r.ok ? '证书已续期' : r.data.message);
    render();
  }
  async function revokeCert(id) {
    if (!confirm('确认吊销该客户端证书（加入 blocklist）？')) return;
    const r = await api('/api/clients/' + id + '/revoke', { method: 'POST' });
    setMsg(r.ok ? '证书已吊销' : r.data.message);
    render();
  }
  async function delClient(id) { if (!confirm('确认删除该客户端？')) return; await api('/api/clients/' + id, { method: 'DELETE' }); await loadAll(); render(); }
  async function delNetwork(id) { if (!confirm('确认删除该网络及其所有资源？')) return; await api('/api/networks/' + id, { method: 'DELETE' }); await loadAll(); render(); }
  async function delServerNode(id) { if (!confirm('确认删除该节点？')) return; await api('/api/server-nodes/' + id, { method: 'DELETE' }); await loadAll(); render(); }
  async function delRoute(id) { if (!confirm('确认删除该路由？')) return; await api('/api/routes/' + id, { method: 'DELETE' }); await loadRoutes(); render(); }
  async function delEdgeService(id) { if (!confirm('确认删除该域名服务？')) return; await api('/api/edge-services/' + id, { method: 'DELETE' }); await loadAll(); render(); }

  boot();
})();
