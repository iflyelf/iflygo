const { test, expect } = require('@playwright/test');
const { execSync } = require('child_process');
const fs = require('fs');
const path = require('path');

// 纯 Go Agent 端到端回归：需要外部启动 Xvfb + iflygo-client crossdesk host。
// 常规 Web E2E 未设置环境变量时自动跳过。
const deviceID = process.env.AGENT_DEVICE_ID || '';
const password = process.env.AGENT_PASSWORD || '';
const hostClipboard = process.env.AGENT_CLIPBOARD_TEXT || '';
const display = process.env.AGENT_DISPLAY || ':99';
const fileRoot = process.env.AGENT_FILE_ROOT || '';

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

    // 多显示器：host_info 填充 Web 客户端的显示选择器。
    await expect.poll(
      () => page.evaluate(() => window.CrossDeskNativeDisplays?.length || 0),
      { timeout: 10000 },
    ).toBeGreaterThan(0);
    const displayOptions = await page.evaluate(() =>
      Array.from(document.getElementById('display-id').options).map(o => o.value));
    expect(displayOptions.length).toBeGreaterThan(0);

    // 剪贴板：主机 -> 浏览器。
    await expect.poll(
      () => page.evaluate(() => window.CrossDeskClipboard?.state),
      { timeout: 15000 },
    ).toBe('ready');
    if (hostClipboard) {
      await expect.poll(
        () => page.evaluate(() => window.CrossDeskClipboard?.remoteText),
        { timeout: 15000 },
      ).toBe(hostClipboard);
    }

    // 画质/帧率协商：host_info 声明支持后，客户端可切换 60 fps。
    await expect.poll(
      () => page.evaluate(() => window.CrossDeskVideoSettings?.supported),
      { timeout: 10000 },
    ).toBe(true);
    await page.evaluate(() => document.querySelector('button[data-setting="frame_rate"][data-value="60"]')?.click());
    await expect.poll(
      () => page.evaluate(() => window.CrossDeskVideoSettings?.selection?.frame_rate),
      { timeout: 10000 },
    ).toBe(60);

    // 键鼠事件通过 DataChannel 发送给 Agent；这里只验证连接后交互不报错，
    // 具体 X11 注入由 Go 集成测试与 Agent 日志覆盖。
    const box = await page.locator('#video').boundingBox();
    expect(box).not.toBeNull();
    await page.mouse.click(box.x + box.width * 0.25, box.y + box.height * 0.25);
    await page.waitForTimeout(300);
    await page.keyboard.press('a');
    await page.keyboard.press('Enter');
    await page.waitForTimeout(300);

    // 文件浏览 / 下载 / 上传（Agent 通过 --file-root 提供目录）。
    if (fileRoot) {
      const realRoot = fs.realpathSync(fileRoot);
      const listing = await page.evaluate(async () => {
        const browser = window.CrossDeskFiles.browser;
        const roots = await browser.listRemote('@roots');
        const entries = await browser.listRemote(roots.entries[0].path);
        return {
          roots: roots.entries.map(entry => entry.path),
          names: entries.entries.map(entry => entry.name),
        };
      });
      expect(listing.roots).toContain(realRoot);
      expect(listing.names).toContain('report.txt');

      const downloadPromise = page.waitForEvent('download', { timeout: 20000 });
      await page.evaluate(async () => {
        const browser = window.CrossDeskFiles.browser;
        const roots = await browser.listRemote('@roots');
        const entries = await browser.listRemote(roots.entries[0].path);
        const file = entries.entries.find(entry => !entry.directory && entry.name === 'report.txt');
        await browser.download([file], '');
      });
      const download = await downloadPromise;
      const downloaded = await download.path();
      expect(fs.readFileSync(downloaded, 'utf8')).toBe('agent-file-e2e');

      // 目录下载：使用 OPFS 作为本机目录，验证 prepare 预留与子目录结构。
      const folderResult = await page.evaluate(async () => {
        const opfs = await navigator.storage.getDirectory();
        await opfs.removeEntry('folder', { recursive: true }).catch(() => {});
        const local = new window.CrossDeskLocalDirectory(opfs);
        const browser = window.CrossDeskFiles.browser;
        browser.selectLocal(local);
        window.CrossDeskFileManager.open(window.CrossDeskFiles);
        const roots = await browser.listRemote('@roots');
        const listing = await browser.listRemote(roots.entries[0].path);
        const folder = listing.entries.find(entry => entry.directory && entry.name === 'folder');
        if (!folder) throw new Error('folder entry missing');
        await browser.download([folder], local.base);
        const read = async parts => {
          let dir = opfs;
          for (const part of parts.slice(0, -1)) dir = await dir.getDirectoryHandle(part);
          const handle = await dir.getFileHandle(parts[parts.length - 1]);
          return (await handle.getFile()).text();
        };
        for (let i = 0; i < 100; i++) {
          try {
            const a = await read(['folder', 'a.txt']);
            const b = await read(['folder', 'sub', 'b.txt']);
            let empty = true;
            try {
              const root = await opfs.getDirectoryHandle('folder');
              await root.getDirectoryHandle('empty');
            } catch (_) { empty = false; }
            return { a, b, empty };
          } catch (_) {}
          await new Promise(resolve => setTimeout(resolve, 100));
        }
        throw new Error('folder download timeout');
      });
      expect(folderResult).toEqual({ a: 'A', b: 'B', empty: true });

      fs.rmSync(path.join(realRoot, 'agent-upload.txt'), { force: true });
      await page.evaluate(async () => {
        const browser = window.CrossDeskFiles.browser;
        const roots = await browser.listRemote('@roots');
        const root = roots.entries[0].path;
        const file = new File(['agent-upload-e2e'], 'agent-upload.txt', { type: 'text/plain' });
        const local = new window.CrossDeskLocalDirectory(null, [file]);
        browser.selectLocal(local);
        await browser.upload([local.base + '/agent-upload.txt'], root);
      });
      await expect.poll(() => {
        const target = path.join(realRoot, 'agent-upload.txt');
        return fs.existsSync(target) ? fs.readFileSync(target, 'utf8') : '';
      }, { timeout: 15000 }).toBe('agent-upload-e2e');
    }

    // 浏览器 -> 主机剪贴板（仅 Linux CI 的 Xvfb 环境验证）。
    if (hostClipboard && process.platform === 'linux') {
      await page.evaluate(() => navigator.clipboard.writeText('browser-to-agent'));
      await expect.poll(() => {
        try {
          return execSync('xclip -selection clipboard -o', {
            env: { ...process.env, DISPLAY: display },
          }).toString();
        } catch (err) {
          return '';
        }
      }, { timeout: 15000 }).toBe('browser-to-agent');
    }

    // 画面仍在持续更新。
    const before = await page.evaluate(() => window.CrossDeskFrameView.frames);
    await expect.poll(
      () => page.evaluate(() => window.CrossDeskFrameView.frames),
      { timeout: 10000 },
    ).toBeGreaterThan(before);
  });
});
