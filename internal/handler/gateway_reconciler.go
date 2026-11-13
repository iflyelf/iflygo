package handler

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/iflyelf/iflygo/internal/logic/task"
	"github.com/iflyelf/iflygo/internal/model"
	"github.com/iflyelf/iflygo/internal/pkg/audit"
	"github.com/iflyelf/iflygo/internal/pkg/scope"
	"github.com/iflyelf/iflygo/internal/protocol"
	"github.com/iflyelf/iflygo/internal/svc"
)

// StartGatewayReconciler 启动网关自动 NAT / 健康切换后台任务。
//
// 每分钟执行一次：
//   - 健康网关变化时递增客户端 config_rev（自动切换 DNS 网关）；
//   - 已匹配 Agent 的网关自动应用 DNS/全量出口 NAT（指纹变化或失败重试）。
func StartGatewayReconciler(ctx context.Context, sc *svc.ServiceContext) {
	go func() {
		select {
		case <-ctx.Done():
			return
		case <-time.After(15 * time.Second):
		}
		runReconcileWithTimeout(ctx, sc)
		t := time.NewTicker(60 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				runReconcileWithTimeout(ctx, sc)
			}
		}
	}()
}

// acquireGatewayLease 通过数据库租约保证多副本只有一个实例执行对账。
// 租约 2 分钟自动过期，避免进程崩溃后永久锁死；不占用数据库连接。
func acquireGatewayLease(sc *svc.ServiceContext) bool {
	now := time.Now().UTC()
	until := now.Add(2 * time.Minute).Format(time.RFC3339)
	res, err := sc.Store.DB.Exec(sc.Store.Rebind(`INSERT INTO gateway_reconcile_lock (id, locked_until) VALUES (1, $1)
		ON CONFLICT (id) DO UPDATE SET locked_until = $3
		WHERE gateway_reconcile_lock.locked_until < $2`), until, now.Format(time.RFC3339), until)
	if err != nil {
		log.Printf("⚠️ 网关对账租约获取失败: %v", err)
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
}

func releaseGatewayLease(sc *svc.ServiceContext) {
	past := time.Now().UTC().Add(-time.Second).Format(time.RFC3339)
	_, _ = sc.Store.DB.Exec(sc.Store.Rebind(`UPDATE gateway_reconcile_lock SET locked_until = $1 WHERE id = 1`), past)
}

// triggerGatewayReconcile 异步触发一次对账（用于 Agent 注册、网关节点创建后立即生效）。
func triggerGatewayReconcile(sc *svc.ServiceContext) {
	go func() {
		time.Sleep(2 * time.Second)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		runGatewayReconcile(ctx, sc)
	}()
}

func runReconcileWithTimeout(ctx context.Context, sc *svc.ServiceContext) {
	rctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	runGatewayReconcile(rctx, sc)
}

var gatewayReconcileBusy atomic.Bool

func runGatewayReconcile(ctx context.Context, sc *svc.ServiceContext) {
	if !gatewayReconcileBusy.CompareAndSwap(false, true) {
		return
	}
	defer gatewayReconcileBusy.Store(false)
	if sc == nil || sc.Settings == nil {
		return
	}
	if !acquireGatewayLease(sc) {
		return
	}
	defer releaseGatewayLease(sc)
	snap := sc.Settings.Snapshot()
	if !snap.Client.AutoNAT {
		return
	}
	nets, err := listNetworksForReconcile(sc)
	if err != nil {
		log.Printf("⚠️ 网关自动 NAT: 读取网络失败: %v", err)
		return
	}
	for _, net := range nets {
		if err := ctx.Err(); err != nil {
			return
		}
		syncGatewayHealthState(sc, net)
		if err := autoApplyNAT(ctx, sc, net); err != nil {
			log.Printf("⚠️ 网关自动 NAT [%s]: %v", net.Name, err)
		}
	}
}

func listNetworksForReconcile(sc *svc.ServiceContext) ([]*model.Network, error) {
	rows, err := sc.Store.DB.Query(sc.Store.Rebind(`SELECT id,name,cidr4,cidr6,gateway4,gateway6,dns,mtu,dns_gateway,egress_mode,egress_exclude,created_at FROM networks ORDER BY id`))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.Network
	for rows.Next() {
		var n model.Network
		if err := rows.Scan(&n.ID, &n.Name, &n.Cidr4, &n.Cidr6, &n.Gateway4, &n.Gateway6,
			&n.DNS, &n.MTU, &n.DNSGateway, &n.EgressMode, &n.EgressExclude, &n.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &n)
	}
	return out, rows.Err()
}

// syncGatewayHealthState 记录当前使用的网关；健康切换时递增客户端配置版本。
func syncGatewayHealthState(sc *svc.ServiceContext, net *model.Network) {
	if effectiveEgressMode(sc, net) == "" {
		return
	}
	gw4, gw6 := "", ""
	if net.Cidr4 != "" {
		gw4 = resolveDNSGateway(sc, net, true)
	}
	if net.Cidr6 != "" {
		gw6 = resolveDNSGateway(sc, net, false)
	}
	if gw4 == "" && gw6 == "" {
		return
	}
	var old4, old6 string
	err := sc.Store.DB.QueryRow(sc.Store.Rebind(`SELECT gateway4,gateway6 FROM gateway_health_state WHERE network_id = $1`), net.ID).Scan(&old4, &old6)
	switch {
	case err == sql.ErrNoRows:
		_, _ = sc.Store.DB.Exec(sc.Store.Rebind(`INSERT INTO gateway_health_state (network_id,gateway4,gateway6,updated_at) VALUES ($1,$2,$3,$4)`),
			net.ID, gw4, gw6, nowStr())
		bumpConfigRev(sc, net.ID)
		log.Printf("🌐 网络 %s 初始网关: v4=%s v6=%s", net.Name, gw4, gw6)
	case err != nil:
		return
	case old4 != gw4 || old6 != gw6:
		_, _ = sc.Store.DB.Exec(sc.Store.Rebind(`UPDATE gateway_health_state SET gateway4=$1,gateway6=$2,updated_at=$3 WHERE network_id=$4`),
			gw4, gw6, nowStr(), net.ID)
		bumpConfigRev(sc, net.ID)
		log.Printf("🔄 网络 %s 网关已切换: v4 %s→%s, v6 %s→%s（客户端将自动拉取）", net.Name, old4, gw4, old6, gw6)
	}
}

func effectiveEgressMode(sc *svc.ServiceContext, net *model.Network) string {
	switch net.EgressMode {
	case "full":
		return "full"
	case "dns":
		return "dns"
	case "off":
		return ""
	}
	// 未显式设置时：全局 DNS 路由开关或任一主机覆盖开启 → dns。
	if sc.Settings != nil && sc.Settings.Snapshot().Client.DNSRoute {
		return "dns"
	}
	rows, err := sc.Store.DB.Query(sc.Store.Rebind(`SELECT client_settings FROM client_nodes WHERE network_id = $1`), net.ID)
	if err != nil {
		return ""
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		if rows.Scan(&raw) != nil || raw == "" {
			continue
		}
		var ov model.ClientOverride
		if json.Unmarshal([]byte(raw), &ov) == nil && ov.DNSRoute != nil && *ov.DNSRoute {
			return "dns"
		}
	}
	return ""
}

type natState struct {
	Fingerprint  string
	Status       string
	LastAttempt  string
	LastError    string
	AlertedAt    string
	RetryCount   int
	TaskID       int64
	ServerNodeID int64
	Gateway      string
	HostIP       string
}

func loadNATState(sc *svc.ServiceContext, networkID int64, mode string) (natState, bool) {
	var st natState
	q := sc.Store.Rebind(`SELECT COALESCE(fingerprint,''), COALESCE(status,''), COALESCE(last_attempt_at,''),
		COALESCE(task_id,0), COALESCE(last_error,''), COALESCE(retry_count,0), COALESCE(alerted_at,''),
		COALESCE(server_node_id,0), COALESCE(gateway,''), COALESCE(host_ip,'')
		FROM gateway_nat_state WHERE network_id = $1 AND mode = $2`)
	if err := sc.Store.DB.QueryRow(q, networkID, mode).Scan(&st.Fingerprint, &st.Status, &st.LastAttempt,
		&st.TaskID, &st.LastError, &st.RetryCount, &st.AlertedAt, &st.ServerNodeID, &st.Gateway, &st.HostIP); err != nil {
		return st, false
	}
	return st, true
}

func saveNATState(sc *svc.ServiceContext, networkID int64, mode string, st natState) {
	now := nowStr()
	_, err := sc.Store.DB.Exec(sc.Store.Rebind(`INSERT INTO gateway_nat_state
		(network_id,mode,fingerprint,server_node_id,gateway,host_ip,task_id,status,last_error,retry_count,alerted_at,last_attempt_at,updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		ON CONFLICT (network_id,mode) DO UPDATE SET
			fingerprint=EXCLUDED.fingerprint, server_node_id=EXCLUDED.server_node_id,
			gateway=EXCLUDED.gateway, host_ip=EXCLUDED.host_ip, task_id=EXCLUDED.task_id,
			status=EXCLUDED.status, last_error=EXCLUDED.last_error, retry_count=EXCLUDED.retry_count,
			alerted_at=EXCLUDED.alerted_at, last_attempt_at=EXCLUDED.last_attempt_at, updated_at=EXCLUDED.updated_at`),
		networkID, mode, st.Fingerprint, st.ServerNodeID, st.Gateway, st.HostIP, st.TaskID,
		st.Status, st.LastError, st.RetryCount, st.AlertedAt, st.LastAttempt, now)
	if err != nil {
		log.Printf("⚠️ 保存网关 NAT 状态失败: %v", err)
	}
}

func maxNATRetries(sc *svc.ServiceContext) int {
	n := 5
	if sc.Settings != nil {
		if v := sc.Settings.Snapshot().Client.AutoNATMaxRetries; v > 0 {
			n = v
		}
	}
	if n < 1 {
		n = 1
	}
	if n > 100 {
		n = 100
	}
	return n
}

// autoApplyNAT 在匹配到 Agent 的健康网关上自动应用 NAT。
func autoApplyNAT(ctx context.Context, sc *svc.ServiceContext, net *model.Network) error {
	if sc.Settings == nil || !sc.Settings.Snapshot().Agent.AllowExecMode {
		return nil
	}
	mode := effectiveEgressMode(sc, net)
	if mode == "" {
		return nil
	}
	var script, gateway string
	var nodeID int64
	var hostIP string

	if mode == "full" {
		gw4, gw6 := "", ""
		if net.Cidr4 != "" {
			gw4 = resolveDNSGateway(sc, net, true)
		}
		if net.Cidr6 != "" {
			gw6 = resolveDNSGateway(sc, net, false)
		}
		if gw4 == "" && gw6 == "" {
			return nil
		}
		gateway = gw4
		if gateway == "" {
			gateway = gw6
		}
		script = buildFullNATScript(net, gw4, gw6, egressExcludePrefixes(sc, net))
	} else {
		v4, v6, gw4, gw6, err := computeDNSNAT(sc, net, "", "")
		if err != nil {
			return nil // 没有公网 DNS 时无需 NAT
		}
		gateway = gw4
		if gateway == "" {
			gateway = gw6
		}
		script = buildDNSNATScript(net, v4, v6, gw4, gw6)
	}
	if gateway == "" {
		return nil
	}
	var pub string
	q := sc.Store.Rebind(`SELECT id, COALESCE(public_host,'') FROM server_nodes
		WHERE network_id = $1 AND (overlay_ip4 = $2 OR overlay_ip6 = $3) ORDER BY id LIMIT 1`)
	if err := sc.Store.DB.QueryRow(q, net.ID, gateway, gateway).Scan(&nodeID, &pub); err != nil {
		return nil // 网关不是服务端节点，无法自动匹配 Agent
	}
	host, err := resolveNATHost(sc, net, "", nodeID)
	if err != nil {
		// 回退：Agent 上报的 overlay 地址（hosts.tunnel_ip）与网关地址一致时也可匹配。
		host, err = resolveNATHost(sc, net, gateway, 0)
	}
	if err != nil {
		return nil // 尚未安装/注册 Agent，安装完成后自动重试
	}
	hostIP = host.IP
	fp := sha256hex(mode + "|" + gateway + "|" + hostIP + "|" + script)

	st, ok := loadNATState(sc, net.ID, mode)
	if ok {
		// 同步任务最终状态，避免重复下发；失败只计一次重试。
		if st.TaskID > 0 {
			var ts string
			if err := sc.Store.DB.QueryRow(sc.Store.Rebind(`SELECT status FROM upgrade_tasks WHERE id = $1`), st.TaskID).Scan(&ts); err == nil && ts != "" && ts != st.Status {
				prevStatus := st.Status
				st.Status = ts
				switch ts {
				case "failed", "timeout":
					st.RetryCount++
					st.LastAttempt = nowStr()
					st.LastError = taskErrorText(sc, st.TaskID, st.LastError)
					if strings.TrimSpace(st.LastError) == "" {
						st.LastError = "任务 #" + strconv.FormatInt(st.TaskID, 10) + " " + ts + "，无命令日志（请检查 Agent 在线状态）"
					}
				case "success":
					st.RetryCount = 0
					st.LastError = ""
					if st.AlertedAt != "" {
						notifyNATRecovered(sc, net, mode, st.Gateway, st.HostIP)
						st.AlertedAt = ""
					}
				case "running", "pending", "draft":
					st.LastAttempt = nowStr()
				}
				_ = prevStatus
				saveNATState(sc, net.ID, mode, st)
			}
		}
		// 配置/网关变化时重新计数，允许重新尝试。
		if st.Fingerprint != fp {
			st.Fingerprint = fp
			st.RetryCount = 0
			st.AlertedAt = ""
			st.Status = ""
			st.LastError = ""
		}
		if st.Fingerprint == fp {
			switch st.Status {
			case "running", "pending", "success", "canceled":
				return nil
			case "failed", "timeout":
				if st.RetryCount >= maxNATRetries(sc) {
					if st.AlertedAt == "" {
						notifyNATFailure(sc, net, mode, gateway, hostIP, st.LastError, st.RetryCount)
						st.AlertedAt = nowStr()
						saveNATState(sc, net.ID, mode, st)
					}
					return nil
				}
				if t, err := time.Parse(time.RFC3339, st.LastAttempt); err == nil && time.Since(t) < 10*time.Minute {
					return nil
				}
			}
		}
	} else {
		st = natState{Fingerprint: fp, ServerNodeID: nodeID, Gateway: gateway, HostIP: hostIP}
	}

	treq := task.CreateRequest{
		Kind:     task.KindCommand,
		Name:     "iflygo 自动 " + strings.ToUpper(mode) + " NAT · " + net.Name,
		Version:  "command",
		Command:  protocol.CommandSpec{Script: script, Interpreter: "/bin/sh", Timeout: "5m"},
		Selector: task.Selector{IPExpr: hostIP},
		Strategy: task.Strategy{Mode: "count", BatchSize: 1, Concurrency: 1, FailureThreshold: "1"},
	}
	treq.SetActor(nil) // 内部后台任务，System scope 允许跨租户
	taskID, cerr := sc.Task.Create(ctx, scope.System(), treq)
	if cerr != nil {
		st.Status = "failed"
		st.LastError = cerr.Error()
		st.RetryCount++
		st.LastAttempt = nowStr()
		if st.RetryCount >= maxNATRetries(sc) && st.AlertedAt == "" {
			notifyNATFailure(sc, net, mode, gateway, hostIP, st.LastError, st.RetryCount)
			st.AlertedAt = nowStr()
		}
		saveNATState(sc, net.ID, mode, st)
		return cerr
	}
	if serr := sc.Task.Start(ctx, scope.System(), taskID); serr != nil {
		st.Status = "failed"
		st.LastError = serr.Error()
		st.RetryCount++
		st.LastAttempt = nowStr()
		if st.RetryCount >= maxNATRetries(sc) && st.AlertedAt == "" {
			notifyNATFailure(sc, net, mode, gateway, hostIP, st.LastError, st.RetryCount)
			st.AlertedAt = nowStr()
		}
		saveNATState(sc, net.ID, mode, st)
		return serr
	}
	st.Fingerprint = fp
	st.Status = "running"
	st.TaskID = taskID
	st.ServerNodeID = nodeID
	st.Gateway = gateway
	st.HostIP = hostIP
	st.LastAttempt = nowStr()
	st.LastError = ""
	saveNATState(sc, net.ID, mode, st)
	log.Printf("🛡️ 网络 %s 已自动下发 %s NAT 任务 #%d → %s（第 %d 次）", net.Name, mode, taskID, hostIP, st.RetryCount+1)
	return nil
}

// taskErrorText 返回任务最近一次错误信息（任务表未保存时回退旧错误）。
func taskErrorText(sc *svc.ServiceContext, taskID int64, fallback string) string {
	var logText string
	_ = sc.Store.DB.QueryRow(sc.Store.Rebind(`SELECT COALESCE(log,'') FROM commands WHERE task_id = $1 ORDER BY id DESC LIMIT 1`), taskID).Scan(&logText)
	logText = strings.TrimSpace(logText)
	if logText == "" {
		return fallback
	}
	if len(logText) > 500 {
		logText = logText[len(logText)-500:]
	}
	return logText
}

func notifyNATFailure(sc *svc.ServiceContext, net *model.Network, mode, gateway, hostIP, errMsg string, retries int) {
	log.Printf("🚨 网关 NAT 连续失败，已停止自动重试: 网络=%s 模式=%s 网关=%s 主机=%s 次数=%d 错误=%s",
		net.Name, mode, gateway, hostIP, retries, errMsg)
	ev := audit.Event{
		Action: audit.ActionGatewayNATFailed, Username: "system", TenantID: 1,
		ResourceType: "network", ResourceID: strconv.FormatInt(net.ID, 10), ResourceName: net.Name,
		Success: false, Message: errMsg,
		Details: map[string]any{"mode": mode, "gateway": gateway, "host_ip": hostIP, "retries": retries},
	}
	if sc.Audit != nil {
		sc.Audit.Log(context.Background(), ev)
	}
	notifyNATWebhook(sc, map[string]any{
		"event": "gateway.nat_failed", "network": net.Name, "mode": mode,
		"gateway": gateway, "host_ip": hostIP, "retries": retries, "error": errMsg,
		"time": nowStr(),
	})
}

func notifyNATRecovered(sc *svc.ServiceContext, net *model.Network, mode, gateway, hostIP string) {
	log.Printf("✅ 网关 NAT 已恢复: 网络=%s 模式=%s 网关=%s 主机=%s", net.Name, mode, gateway, hostIP)
	ev := audit.Event{
		Action: audit.ActionGatewayNATRecovered, Username: "system", TenantID: 1,
		ResourceType: "network", ResourceID: strconv.FormatInt(net.ID, 10), ResourceName: net.Name,
		Success: true, Details: map[string]any{"mode": mode, "gateway": gateway, "host_ip": hostIP},
	}
	if sc.Audit != nil {
		sc.Audit.Log(context.Background(), ev)
	}
	notifyNATWebhook(sc, map[string]any{
		"event": "gateway.nat_recovered", "network": net.Name, "mode": mode,
		"gateway": gateway, "host_ip": hostIP, "time": nowStr(),
	})
}

func notifyNATWebhook(sc *svc.ServiceContext, payload map[string]any) {
	if sc.Settings == nil {
		return
	}
	raw := strings.TrimSpace(sc.Settings.Snapshot().Client.AutoNATWebhook)
	if raw == "" {
		return
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, raw, bytes.NewReader(b))
		if err != nil {
			log.Printf("⚠️ NAT 告警 Webhook 构造失败: %v", err)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			log.Printf("⚠️ NAT 告警 Webhook 发送失败: %v", err)
			return
		}
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, resp.Body)
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			log.Printf("⚠️ NAT 告警 Webhook 返回 %d", resp.StatusCode)
		}
	}()
}
