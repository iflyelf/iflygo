const { test, expect } = require('@playwright/test');

const ADMIN = process.env.E2E_ADMIN || 'admin';
const PASSWORD = process.env.E2E_PASSWORD || 'admin123';

// 登录接口有限流（防暴力破解）：每个 worker 只走一次表单登录，之后复用会话 Cookie。
let sessionCookies = null;

async function login(page, user = ADMIN, pass = PASSWORD) {
  if (sessionCookies) {
    await page.context().addCookies(sessionCookies);
    await page.goto('/');
    await expect(page.locator('.main-nav')).toBeVisible();
    return;
  }
  await page.goto('/');
  await page.locator('input[autocomplete="username"]').fill(user);
  await page.locator('input[autocomplete="current-password"]').fill(pass);
  await page.getByRole('button', { name: '登录', exact: true }).click();
  await expect(page.locator('.main-nav')).toBeVisible();
  sessionCookies = await page.context().cookies();
}

// 打开「隧道」顶栏下拉并切换到指定子页。
async function openTunnel(page, section) {
  await login(page);
  await page.locator('.main-nav .nav-link', { hasText: '隧道' }).click();
  await page.locator('.main-nav .nav-menu-item', { hasText: section }).click();
}

function autoAccept(page) {
  page.on('dialog', (d) => d.accept());
}

test('登录失败提示', async ({ page }) => {
  await page.goto('/');
  await page.locator('input[autocomplete="username"]').fill(ADMIN);
  await page.locator('input[autocomplete="current-password"]').fill('wrong-password');
  await page.getByRole('button', { name: '登录', exact: true }).click();
  await expect(page.locator('.err')).toContainText('用户名或密码错误');
});

test('登录后显示升级控制平台与主机列表', async ({ page }) => {
  await login(page);
  await expect(page.locator('.brand-text')).toHaveText('升级控制平台');
  // 默认进入主机页，展示接入主机卡片
  await expect(page.getByText('接入主机（安装 Agent）').first()).toBeVisible();
});

test('三主题切换与持久化', async ({ page }) => {
  await login(page);
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'warm');
  await page.locator('.theme-btn[title="冷蓝"]').click();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'cool');
  await page.locator('.theme-btn[title="暗黑"]').click();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  // localStorage 持久化 + 刷新恢复（upgrade_mgr 使用 upgrade_mgr_theme 键）
  const saved = await page.evaluate(() => localStorage.getItem('upgrade_mgr_theme'));
  expect(saved).toBe('dark');
  await page.reload();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  await page.locator('.theme-btn[title="暖沙米"]').click();
});

test('隧道管理：创建网络与客户端', async ({ page }) => {
  autoAccept(page);
  const suffix = Date.now();

  // 创建网络
  await openTunnel(page, '网络');
  await expect(page.getByRole('heading', { name: '网络', exact: true })).toBeVisible();
  await page.getByPlaceholder('名称').fill('e2e-net-' + suffix);
  await page.getByPlaceholder('IPv4 CIDR，如 10.77.0.0/16').fill('10.77.0.0/16');
  await page.getByRole('button', { name: '创建网络', exact: true }).click();
  await expect(page.locator('table')).toContainText('e2e-net-' + suffix);

  // 创建客户端
  await openTunnel(page, '客户端');
  await expect(page.getByRole('heading', { name: '客户端', exact: true })).toBeVisible();
  await page.getByPlaceholder('名称').fill('e2e-client-' + suffix);
  await page.getByRole('button', { name: '创建客户端', exact: true }).click();
  await expect(page.locator('table')).toContainText('e2e-client-' + suffix);
});

test('隧道管理：五个子页可切换', async ({ page }) => {
  for (const section of ['客户端', '网络', '服务端节点', '路由', '边缘服务']) {
    await openTunnel(page, section);
    await expect(page.locator('.card h3').first()).toBeVisible();
  }
});
