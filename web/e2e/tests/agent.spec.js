const { test, expect } = require('@playwright/test');

// 纯 Go Agent 端到端回归：需要外部启动 Xvfb + iflygo-client crossdesk host。
// 常规 Web E2E 未设置环境变量时自动跳过。
const deviceID = process.env.AGENT_DEVICE_ID || '';
const password = process.env.AGENT_PASSWORD || '';

test.describe('CrossDesk 纯 Go Agent', () => {
  test.skip(!deviceID || !password, '未提供 AGENT_DEVICE_ID / AGENT_PASSWORD');

  test('接收 JPEG 画面并完成键鼠交互', async ({ page }) => {
    await page.goto(`/crossdesk/web/?device_id=${encodeURIComponent(deviceID)}`);
    await page.fill('#transmission-pwd', password);
    await expect(page.locator('#connect')).toBeEnabled();
    await page.click('#connect');

    await expect.poll(
      () => page.evaluate(() => window.CrossDeskFrameView?.frames || 0),
      { timeout: 30000 },
    ).toBeGreaterThan(2);

    const canvas = await page.evaluate(() => {
      const el = document.getElementById('native-frame-canvas');
      return el ? { width: el.width, height: el.height, display: el.style.display } : null;
    });
    expect(canvas).not.toBeNull();
    expect(canvas.width).toBeGreaterThan(0);
    expect(canvas.height).toBeGreaterThan(0);
    expect(canvas.display).toBe('block');

    // 键鼠事件通过 DataChannel 发送给 Agent；这里只验证连接后交互不报错，
    // 具体 X11 注入由 Go 集成测试与 Agent 日志覆盖。
    const box = await page.locator('#video').boundingBox();
    expect(box).not.toBeNull();
    await page.mouse.click(box.x + box.width * 0.25, box.y + box.height * 0.25);
    await page.waitForTimeout(300);
    await page.keyboard.press('a');
    await page.keyboard.press('Enter');
    await page.waitForTimeout(300);

    // 画面仍在持续更新。
    const before = await page.evaluate(() => window.CrossDeskFrameView.frames);
    await expect.poll(
      () => page.evaluate(() => window.CrossDeskFrameView.frames),
      { timeout: 10000 },
    ).toBeGreaterThan(before);
  });
});
