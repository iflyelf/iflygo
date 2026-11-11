// Package hostmerge 自动合并重复主机。
//
// 重复来源：多网卡/重装 Agent 时来源 IP 与清单 IP 不一致，可能产生同一台机器的
// 多条记录。合并键（按优先级）：
//   - tunnel_ip：管理员手工填写的隧道地址，唯一性强；
//   - exit_ip + hostname：Agent 上报的出口 IP，且主机名一致（避免 NAT 下误合并）。
//
// 合并时把 Agent、任务主机记录、指令迁移到主记录，业务字段/自定义字段按
// 「主记录优先、空值补齐」合并，然后删除重复行。不会删除主记录。
package hostmerge

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/iflyelf/iflygo/internal/pkg/pglock"
	"github.com/iflyelf/iflygo/internal/store"
)

// 跨副本互斥键（任意稳定 int64）。
const lockKey = int64(0x484F53544D455247) // "HOSTMERG"

func isSQLite() bool { return store.CurrentDialect() == store.SQLite }

// mergedArray 返回两个数组列去重合并的 SQL 表达式。
// PG 用 unnest + ARRAY；SQLite 的数组列是 JSON 文本，用 json_each。
func mergedArray(a, b string) string {
	if isSQLite() {
		return fmt.Sprintf(`(SELECT json_group_array(DISTINCT value) FROM (
			SELECT value FROM json_each(COALESCE(%s,'[]'))
			UNION ALL
			SELECT value FROM json_each(COALESCE(%s,'[]'))))`, a, b)
	}
	return fmt.Sprintf(`ARRAY(SELECT DISTINCT x FROM unnest(%s || %s) x)`, a, b)
}

// greatest/least 返回 NULL 安全的比较表达式。
// PG 的 GREATEST/LEAST 会忽略 NULL；SQLite 的 MAX/MIN 在任一参数为 NULL 时返回 NULL，
// 因此这里用 CASE 保持与 PG 一致。
func greatest(a, b string) string {
	if isSQLite() {
		return fmt.Sprintf(`(CASE WHEN %s IS NULL THEN %s WHEN %s IS NULL THEN %s WHEN %s > %s THEN %s ELSE %s END)`, a, b, a, b, a, b, a, b)
	}
	return fmt.Sprintf(`GREATEST(%s,%s)`, a, b)
}

func least(a, b string) string {
	if isSQLite() {
		return fmt.Sprintf(`(CASE WHEN %s IS NULL THEN %s WHEN %s IS NULL THEN %s WHEN %s < %s THEN %s ELSE %s END)`, a, b, a, b, a, b, a, b)
	}
	return fmt.Sprintf(`LEAST(%s,%s)`, a, b)
}

// mergedJSON 返回两个 JSON 对象合并的 SQL 表达式（第二个参数优先）。
func mergedJSON(a, b string) string {
	if isSQLite() {
		// json_patch 的后者覆盖前者，与 PG 的 a || b 语义一致。
		return fmt.Sprintf(`json_patch(COALESCE(%s,'{}'), COALESCE(%s,'{}'))`, a, b)
	}
	return fmt.Sprintf(`COALESCE(%s,'{}'::jsonb) || COALESCE(%s,'{}'::jsonb)`, a, b)
}

// MergeCandidates 将 candidates 合并到 target（调用方事务）。
//
// 只允许同一租户内合并；target 不会删除。返回实际合并（删除）的行数。
func MergeCandidates(ctx context.Context, tx *sql.Tx, targetID int64, candidates []int64) (int, error) {
	if tx == nil || targetID <= 0 {
		return 0, fmt.Errorf("合并参数无效")
	}
	ids := uniquePositive(candidates)
	if len(ids) == 0 {
		return 0, nil
	}
	// 统一按 id 升序加锁，避免与其它合并/删除形成反向锁序。
	if _, err := tx.ExecContext(ctx,
		store.SQL(`SELECT id FROM hosts WHERE id = ANY($1) ORDER BY id FOR UPDATE`), store.Array(ids)); err != nil {
		return 0, fmt.Errorf("锁定主机失败: %w", err)
	}
	// 租户一致性：跨租户绝不合并。
	var tenants []int64
	rows, err := tx.QueryContext(ctx,
		store.SQL(`SELECT DISTINCT tenant_id FROM hosts WHERE id = ANY($1)`), store.Array(ids))
	if err != nil {
		return 0, err
	}
	for rows.Next() {
		var t int64
		if err := rows.Scan(&t); err != nil {
			rows.Close()
			return 0, err
		}
		tenants = append(tenants, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(tenants) != 1 {
		return 0, fmt.Errorf("候选主机不属于同一租户，拒绝合并")
	}

	merged := 0
	for _, id := range ids {
		if id == targetID {
			continue
		}
		ok, err := mergeOne(ctx, tx, targetID, id)
		if err != nil {
			return merged, err
		}
		if ok {
			merged++
		}
	}
	return merged, nil
}

// mergeOne 合并一台重复主机到 target。
func mergeOne(ctx context.Context, tx *sql.Tx, targetID, dupID int64) (bool, error) {
	query := fmt.Sprintf(`
		UPDATE hosts t SET
			hostname = COALESCE(NULLIF(t.hostname,''), d.hostname),
			consul_node = COALESCE(NULLIF(t.consul_node,''), d.consul_node),
			datacenter = COALESCE(NULLIF(t.datacenter,''), d.datacenter),
			os_family = COALESCE(NULLIF(t.os_family,''), d.os_family),
			os_version = COALESCE(NULLIF(t.os_version,''), d.os_version),
			kernel = COALESCE(NULLIF(t.kernel,''), d.kernel),
			arch = COALESCE(NULLIF(t.arch,''), d.arch),
			account = COALESCE(NULLIF(t.account,''), d.account),
			app_code = COALESCE(NULLIF(t.app_code,''), d.app_code),
			biz_group = COALESCE(NULLIF(t.biz_group,''), d.biz_group),
			department = COALESCE(NULLIF(t.department,''), d.department),
			vendor = COALESCE(NULLIF(t.vendor,''), d.vendor),
			os_type = COALESCE(NULLIF(t.os_type,''), d.os_type),
			host_type = COALESCE(NULLIF(t.host_type,''), d.host_type),
			tags = %s,
			agent_id = CASE WHEN COALESCE(t.last_seen,'epoch'::timestamptz) < COALESCE(d.last_seen,'epoch'::timestamptz)
			                THEN COALESCE(NULLIF(d.agent_id,''), t.agent_id)
			                ELSE COALESCE(NULLIF(t.agent_id,''), d.agent_id) END,
			agent_version = COALESCE(NULLIF(t.agent_version,''), d.agent_version),
			agent_status = CASE WHEN COALESCE(t.last_seen,'epoch'::timestamptz) < COALESCE(d.last_seen,'epoch'::timestamptz)
			                        AND COALESCE(d.agent_id,'') <> ''
			                    THEN d.agent_status ELSE t.agent_status END,
			supervisor = COALESCE(NULLIF(t.supervisor,''), d.supervisor),
			component_versions = %s,
			maintenance_until = %s,
			maintenance_reason = CASE WHEN COALESCE(d.maintenance_until,'epoch'::timestamptz) > COALESCE(t.maintenance_until,'epoch'::timestamptz)
			                          THEN d.maintenance_reason ELSE t.maintenance_reason END,
			last_stage = COALESCE(NULLIF(t.last_stage,''), d.last_stage),
			last_seen = %s,
			last_synced_at = %s,
			created_at = %s,
			agent_build_version = COALESCE(NULLIF(t.agent_build_version,''), d.agent_build_version),
			agent_build_hash = COALESCE(NULLIF(t.agent_build_hash,''), d.agent_build_hash),
			agent_build_variant = COALESCE(NULLIF(t.agent_build_variant,''), d.agent_build_variant),
			exit_ip = COALESCE(NULLIF(t.exit_ip,''), d.exit_ip),
			tunnel_ip = COALESCE(NULLIF(t.tunnel_ip,''), d.tunnel_ip),
			server_url_override = COALESCE(NULLIF(t.server_url_override,''), d.server_url_override),
			agent_server_url = COALESCE(NULLIF(t.agent_server_url,''), d.agent_server_url),
			agent_default_server_url = COALESCE(NULLIF(t.agent_default_server_url,''), d.agent_default_server_url),
			tunnel_check_ok = CASE WHEN COALESCE(t.tunnel_check_at,'epoch'::timestamptz) < COALESCE(d.tunnel_check_at,'epoch'::timestamptz)
			                       THEN d.tunnel_check_ok ELSE t.tunnel_check_ok END,
			tunnel_check_source = COALESCE(NULLIF(t.tunnel_check_source,''), d.tunnel_check_source),
			tunnel_check_at = %s,
			tunnel_check_latency_ms = CASE WHEN COALESCE(t.tunnel_check_at,'epoch'::timestamptz) < COALESCE(d.tunnel_check_at,'epoch'::timestamptz)
			                               THEN d.tunnel_check_latency_ms ELSE t.tunnel_check_latency_ms END,
			tunnel_check_detail = COALESCE(NULLIF(t.tunnel_check_detail,''), d.tunnel_check_detail),
			tunnel_check_requested_at = %s,
			custom_fields = %s,
			source_group = COALESCE(NULLIF(t.source_group,''), d.source_group),
			source_services = %s,
			updated_at = NOW()
		FROM hosts d
		WHERE t.id=$1 AND d.id=$2`,
		mergedArray("t.tags", "d.tags"),
		mergedJSON("d.component_versions", "t.component_versions"),
		greatest("t.maintenance_until", "d.maintenance_until"),
		greatest("t.last_seen", "d.last_seen"),
		greatest("t.last_synced_at", "d.last_synced_at"),
		least("t.created_at", "d.created_at"),
		greatest("t.tunnel_check_at", "d.tunnel_check_at"),
		greatest("t.tunnel_check_requested_at", "d.tunnel_check_requested_at"),
		mergedJSON("d.custom_fields", "t.custom_fields"),
		mergedArray("t.source_services", "d.source_services"))
	res, err := tx.ExecContext(ctx, store.SQL(query), targetID, dupID)
	if err != nil {
		return false, fmt.Errorf("合并主机字段失败: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return false, nil
	}

	// 任务主机记录：同一任务下保留更“终态”的结果，再迁移剩余记录。
	if _, err := tx.ExecContext(ctx, store.SQL(`
		UPDATE task_hosts t SET
			status=d.status, stage=d.stage, from_version=d.from_version, to_version=d.to_version,
			attempts=GREATEST(t.attempts,d.attempts), exit_code=d.exit_code, log=d.log,
			started_at=COALESCE(t.started_at,d.started_at), finished_at=COALESCE(d.finished_at,t.finished_at)
		FROM task_hosts d
		WHERE t.host_id=$1 AND d.host_id=$2 AND t.task_id=d.task_id
		  AND t.status IN ('pending','running') AND d.status NOT IN ('pending','running')`),
		targetID, dupID); err != nil {
		return false, fmt.Errorf("合并任务记录失败: %w", err)
	}
	if _, err := tx.ExecContext(ctx, store.SQL(`
		DELETE FROM task_hosts d
		WHERE d.host_id=$2 AND EXISTS (
			SELECT 1 FROM task_hosts t WHERE t.host_id=$1 AND t.task_id=d.task_id)`),
		targetID, dupID); err != nil {
		return false, fmt.Errorf("清理重复任务记录失败: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE task_hosts SET host_id=$1 WHERE host_id=$2`, targetID, dupID); err != nil {
		return false, fmt.Errorf("迁移任务记录失败: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE commands SET host_id=$1 WHERE host_id=$2`, targetID, dupID); err != nil {
		return false, fmt.Errorf("迁移指令失败: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE agents SET host_id=$1 WHERE host_id=$2`, targetID, dupID); err != nil {
		return false, fmt.Errorf("迁移 Agent 失败: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM hosts WHERE id=$1`, dupID); err != nil {
		return false, fmt.Errorf("删除重复主机失败: %w", err)
	}
	return true, nil
}

// PickTarget 从候选主机中选出主记录：
// 精确 ip=key > 有 Agent > consul_mgr/manual 来源 > 业务字段更完整 > 最近心跳 > id 最小。
func PickTarget(ctx context.Context, tx *sql.Tx, ids []int64, key string) (int64, error) {
	ids = uniquePositive(ids)
	if len(ids) == 0 {
		return 0, fmt.Errorf("没有候选主机")
	}
	rows, err := tx.QueryContext(ctx, store.SQL(`
		SELECT id, ip, COALESCE(agent_id,''), COALESCE(source,''), COALESCE(hostname,''),
		       COALESCE(app_code,''), COALESCE(biz_group,''), COALESCE(account,''),
		       COALESCE(department,''), COALESCE(vendor,''), COALESCE(host_type,''),
		       last_seen
		FROM hosts WHERE id = ANY($1) ORDER BY id`), store.Array(ids))
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	type cand struct {
		id       int64
		score    int
		lastSeen time.Time
	}
	var cands []cand
	for rows.Next() {
		var (
			id                          int64
			ip, agent, source, hostname string
			app, biz, account, dept     string
			vendor, htype               string
			lastSeen                    store.NullTime
		)
		if err := rows.Scan(&id, &ip, &agent, &source, &hostname, &app, &biz,
			&account, &dept, &vendor, &htype, &lastSeen); err != nil {
			return 0, err
		}
		score := 0
		if key != "" && ip == key {
			score += 1000
		}
		// 优先保留清单行（consul_mgr/manual），保留其 ip 与业务字段；
		// Agent 行作为补充合并进来。
		switch source {
		case "consul_mgr":
			score += 300
		case "manual":
			score += 200
		}
		if agent != "" {
			score += 100
		}
		for _, v := range []string{hostname, app, biz, account, dept, vendor, htype} {
			if strings.TrimSpace(v) != "" {
				score += 5
			}
		}
		var seen time.Time
		if lastSeen.Valid {
			seen = lastSeen.Time
		}
		cands = append(cands, cand{id: id, score: score, lastSeen: seen})
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(cands) == 0 {
		return 0, fmt.Errorf("候选主机不存在")
	}
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].score != cands[j].score {
			return cands[i].score > cands[j].score
		}
		if !cands[i].lastSeen.Equal(cands[j].lastSeen) {
			return cands[i].lastSeen.After(cands[j].lastSeen)
		}
		return cands[i].id < cands[j].id
	})
	return cands[0].id, nil
}

// AutoMergeAll 扫描并自动合并全部重复主机（跨副本互斥）。
//
// 返回合并（删除）的主机行数。未获取到锁时返回 0, nil（其他副本正在处理）。
func AutoMergeAll(ctx context.Context, db *sql.DB) (int, error) {
	if db == nil {
		return 0, nil
	}
	lockCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	lock, ok, err := pglock.Try(lockCtx, db, lockKey)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, nil
	}
	defer lock.Unlock()

	agg := "array_agg(id ORDER BY id)"
	if isSQLite() {
		agg = "json_group_array(id)"
	}
	// tunnel_ip 分组（强身份）；exit_ip + hostname 分组（避免 NAT 误合并）。
	rows, err := db.QueryContext(ctx, store.SQL(fmt.Sprintf(`
		SELECT tenant_id, 'tunnel'::text, tunnel_ip, %s
		FROM hosts
		WHERE tunnel_ip <> ''
		GROUP BY tenant_id, tunnel_ip
		HAVING count(*) > 1
		UNION ALL
		SELECT tenant_id, 'exit'::text, exit_ip, %s
		FROM hosts
		WHERE exit_ip <> '' AND hostname <> ''
		GROUP BY tenant_id, exit_ip, hostname
		HAVING count(*) > 1`, agg, agg)))
	if err != nil {
		return 0, err
	}
	type group struct {
		tenant int64
		kind   string
		key    string
		ids    store.Int64Array
	}
	var groups []group
	for rows.Next() {
		var g group
		if err := rows.Scan(&g.tenant, &g.kind, &g.key, &g.ids); err != nil {
			rows.Close()
			return 0, err
		}
		groups = append(groups, g)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	total := 0
	for _, g := range groups {
		ids := []int64(g.ids)
		if len(ids) < 2 {
			continue
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return total, err
		}
		target, err := PickTarget(ctx, tx, ids, g.key)
		if err != nil {
			tx.Rollback()
			log.Printf("⚠️ 自动合并主机失败（%s %s）: %v", g.kind, g.key, err)
			continue
		}
		n, err := MergeCandidates(ctx, tx, target, ids)
		if err != nil {
			tx.Rollback()
			log.Printf("⚠️ 自动合并主机失败（%s %s）: %v", g.kind, g.key, err)
			continue
		}
		if err := tx.Commit(); err != nil {
			log.Printf("⚠️ 自动合并提交失败（%s %s）: %v", g.kind, g.key, err)
			continue
		}
		if n > 0 {
			log.Printf("🧹 自动合并重复主机：%s=%s 合并 %d 台到 ID %d", g.kind, g.key, n, target)
			total += n
		}
	}
	return total, nil
}

func uniquePositive(ids []int64) []int64 {
	seen := map[int64]bool{}
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
