(() => {
  'use strict';

  const state = {
    user: null,
    view: 'loading',
    networks: [],
    clients: [],
    msg: '',
    msgKind: 'err',
  };

  const app = document.getElementById('app');

  // ---------- 工具 ----------
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
    if (res.status === 401 && state.user) {
      state.user = null;
      state.view = 'login';
    }
    return { ok: res.ok, status: res.status, data };
  }

  function setMsg(text, kind) {
    state.msg = text || '';
    state.msgKind = kind || 'err';
  }

  // ---------- 数据加载 ----------
  async function loadAll() {
    const [n, c] = await Promise.all([
      api('/api/networks'),
      api('/api/clients'),
    ]);
    state.networks = n.ok ? n.data : [];
    state.clients = c.ok ? c.data : [];
  }

  async function boot() {
    const u = await api('/api/auth/userinfo');
    if (u.ok && u.data && u.data.username) {
      state.user = u.data;
      state.view = 'dashboard';
      await loadAll();
    } else {
      state.view = 'login';
    }
    render();
  }

  // ---------- 视图 ----------
  function render() {
    app.replaceChildren();
    if (state.view === 'loading') {
      app.append(h('div', { class: 'card' }, '加载中…'));
      return;
    }
    if (state.view === 'login') return renderLogin();
    renderDashboard();
  }

  function renderLogin() {
    const user = h('input', { type: 'text', placeholder: '用户名', autocomplete: 'username' });
    const pass = h('input', { type: 'password', placeholder: '密码', autocomplete: 'current-password' });
    const btn = h('button', { class: 'btn primary', style: 'width:100%' }, '登录');

    const submit = async () => {
      const r = await api('/api/auth/login', { method: 'POST', body: { username: user.value, password: pass.value } });
      if (r.ok) {
        state.user = r.data;
        state.view = 'dashboard';
        await loadAll();
        render();
      } else {
        setMsg(r.data.message || '登录失败');
        render();
      }
    };
    btn.onclick = submit;
    pass.onkeydown = (e) => { if (e.key === 'Enter') submit(); };

    app.append(h('div', { class: 'card', style: 'max-width:360px;margin:80px auto' },
      h('h1', {}, 'iFlyGo'),
      msgEl(),
      user, pass, btn,
    ));
  }

  function msgEl() {
    if (!state.msg) return null;
    return h('div', { class: 'msg ' + state.msgKind }, state.msg);
  }

  function renderDashboard() {
    const logout = h('button', { class: 'btn sm' }, '退出');
    logout.onclick = async () => {
      await api('/api/auth/logout', { method: 'POST' });
      state.user = null;
      state.view = 'login';
      render();
    };

    app.append(
      h('div', { class: 'topbar' },
        h('span', { class: 'brand' }, 'iFlyGo 控制台'),
        h('div', {}, h('span', { class: 'muted' }, state.user.username + '  '), logout),
      ),
      msgEl(),
      h('div', { class: 'grid' }, renderNetworks(), renderClients()),
    );
  }

  function renderNetworks() {
    const rows = state.networks.map((n) =>
      h('tr', {},
        h('td', {}, n.name),
        h('td', {}, n.cidr4),
        h('td', {}, n.cidr6 || '—'),
        h('td', {},
          h('button', { class: 'btn danger sm', onclick: () => delNetwork(n.id) }, '删除'),
        ),
      ),
    );

    const name = h('input', { placeholder: '名称' });
    const cidr4 = h('input', { placeholder: 'IPv4 CIDR, 如 10.88.0.0/16' });
    const cidr6 = h('input', { placeholder: 'IPv6 CIDR (可选), 如 fd88::/64' });
    const gw4 = h('input', { placeholder: 'IPv4 网关 (可选)' });
    const gw6 = h('input', { placeholder: 'IPv6 网关 (可选)' });
    const create = h('button', { class: 'btn primary', style: 'width:100%' }, '创建网络');
    create.onclick = async () => {
      const r = await api('/api/networks', {
        method: 'POST',
        body: { name: name.value, cidr4: cidr4.value, cidr6: cidr6.value, gateway4: gw4.value, gateway6: gw6.value },
      });
      if (r.ok) { await loadAll(); setMsg('网络已创建', 'ok'); } else { setMsg(r.data.message); }
      render();
    };

    return h('div', { class: 'card' },
      h('h2', {}, '网络'),
      h('table', {}, h('thead', {}, h('tr', {}, h('th', {}, '名称'), h('th', {}, 'IPv4'), h('th', {}, 'IPv6'), h('th', {}))),
        h('tbody', {}, rows)),
      h('div', { class: 'muted', style: 'margin-bottom:8px' }, '新建网络'),
      h('div', { class: 'form-row' }, name, cidr4),
      h('div', { class: 'form-row' }, cidr6),
      h('div', { class: 'form-row' }, gw4, gw6),
      create,
    );
  }

  function renderClients() {
    const rows = state.clients.map((c) =>
      h('tr', {},
        h('td', {}, c.name),
        h('td', {}, c.ipv4 || '—'),
        h('td', {}, c.ipv6 || '—'),
        h('td', {}, h('span', { class: 'badge ' + (c.status === 'online' ? 'online' : 'offline') }, c.status)),
        h('td', {},
          h('button', { class: 'btn sm', onclick: () => getToken(c.id, c.name) }, '安装命令'),
          ' ',
          h('button', { class: 'btn danger sm', onclick: () => delClient(c.id) }, '删除'),
        ),
      ),
    );

    const name = h('input', { placeholder: '名称' });
    const net = h('select', {}, ...state.networks.map((n) => h('option', { value: n.id }, n.name + ' (' + n.cidr4 + ')')));
    const ipv4 = h('input', { placeholder: 'IPv4 (留空自动分配)' });
    const ipv6 = h('input', { placeholder: 'IPv6 (留空自动分配)' });
    const create = h('button', { class: 'btn primary', style: 'width:100%' }, '创建客户端');
    create.onclick = async () => {
      const r = await api('/api/clients', {
        method: 'POST',
        body: { network_id: Number(net.value), name: name.value, ipv4: ipv4.value, ipv6: ipv6.value },
      });
      if (r.ok) {
        await loadAll();
        setMsg('客户端已创建，安装命令已复制：' + r.data.install_command, 'ok');
        try { navigator.clipboard.writeText(r.data.install_command); } catch (_) {}
      } else { setMsg(r.data.message); }
      render();
    };

    return h('div', { class: 'card' },
      h('h2', {}, '客户端'),
      h('table', {}, h('thead', {}, h('tr', {}, h('th', {}, '名称'), h('th', {}, 'IPv4'), h('th', {}, 'IPv6'), h('th', {}, '状态'), h('th', {}))),
        h('tbody', {}, rows)),
      h('div', { class: 'muted', style: 'margin-bottom:8px' }, '新建客户端'),
      net, name, ipv4, ipv6,
      create,
    );
  }

  async function getToken(id, name) {
    const r = await api('/api/clients/' + id + '/enroll-token', { method: 'POST' });
    if (r.ok) {
      setMsg(name + ' 安装命令：' + r.data.install_command, 'ok');
      try { navigator.clipboard.writeText(r.data.install_command); } catch (_) {}
    } else { setMsg(r.data.message); }
    render();
  }

  async function delClient(id) {
    if (!confirm('确认删除该客户端？')) return;
    await api('/api/clients/' + id, { method: 'DELETE' });
    await loadAll();
    render();
  }

  async function delNetwork(id) {
    if (!confirm('确认删除该网络及其所有客户端？')) return;
    await api('/api/networks/' + id, { method: 'DELETE' });
    await loadAll();
    render();
  }

  boot();
})();
