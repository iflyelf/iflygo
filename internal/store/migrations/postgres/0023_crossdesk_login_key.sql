-- CrossDesk 设备登录公钥（Ed25519），启用后必须先通过设备密钥登录。
ALTER TABLE crossdesk_devices ADD COLUMN login_public_key TEXT NOT NULL DEFAULT '';
