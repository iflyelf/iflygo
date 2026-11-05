const { test, expect } = require('@playwright/test');

const ADMIN = process.env.E2E_ADMIN || 'admin';
const PASSWORD = process.env.E2E_PASSWORD || 'admin123';

async function login(page, user = ADMIN, pass = PASSWORD) {
  await page.goto('/');
  await page.locator('input[autocomplete="username"]').fill(user);
  await page.locator('input[autocomplete="current-password"]').fill(pass);
  await page.getByRole('button', { name: '登录', exact: true }).click();
}

test('登录失败提示', async ({ page }) => {
  await page.goto('/');
  await page.locator('input[autocomplete="username"]').fill(ADMIN);
  await page.locator('input[autocomplete="current-password"]').fill('wrong-password');
  await page.getByRole('button', { name: '登录', exact: true }).click();
  await expect(page.locator('.msg.err')).toContainText('用户名或密码错误');
});

test('登录后导航与三主题切换', async ({ page }) => {
  await login(page);
  await expect(page.locator('.brand')).toHaveText('iFlyGo 控制台');

  // 默认暖沙米
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'warm');

  // 冷蓝 → 暗黑 → 暖沙米
  await page.locator('.theme-btn[title="冷蓝"]').click();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'cool');
  await page.locator('.theme-btn[title="暗黑"]').click();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  await page.locator('.theme-btn[title="暖沙米"]').click();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'warm');

  // localStorage 持久化 + 刷新恢复
  await page.locator('.theme-btn[title="暗黑"]').click();
  const saved = await page.evaluate(() => localStorage.getItem('iflygo_theme'));
  expect(saved).toBe('dark');
  await page.reload();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  await page.locator('.theme-btn[title="暖沙米"]').click();

  // 顶部页签导航
  await page.locator('.tab', { hasText: '网络' }).click();
  await expect(page.getByRole('heading', { name: '网络' })).toBeVisible();
  await page.locator('.tab', { hasText: '客户端' }).click();
  await expect(page.getByRole('heading', { name: '客户端' })).toBeVisible();
  await page.locator('.tab', { hasText: '节点' }).click();
  await expect(page.getByRole('heading', { name: '服务端节点' })).toBeVisible();
});

test('创建网络与客户端', async ({ page }) => {
  await login(page);

  // 创建网络
  await page.locator('.tab', { hasText: '网络' }).click();
  await page.getByPlaceholder('名称').fill('e2e-net');
  await page.getByPlaceholder('IPv4 CIDR, 如 10.77.0.0/16').fill('10.77.0.0/16');
  await page.getByRole('button', { name: '创建网络', exact: true }).click();
  await expect(page.locator('.msg.ok')).toContainText('网络已创建');

  // 创建客户端
  await page.locator('.tab', { hasText: '客户端' }).click();
  await page.getByPlaceholder('名称').fill('e2e-client');
  await page.getByRole('button', { name: '创建客户端', exact: true }).click();
  await expect(page.locator('.msg.ok')).toContainText('已创建');
  await expect(page.locator('table')).toContainText('e2e-client');
});
