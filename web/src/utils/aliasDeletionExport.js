function csvCell(value) {
  let text = value == null ? "" : String(value);
  if (/^[\s\u0000-\u001f]*[=+\-@\t\r]/.test(text)) text = "'" + text;
  return `"${text.replaceAll('"', '""')}"`;
}

export function buildAliasDeletionCsv(job) {
  const rows = [["任务 ID", "邮箱", "状态", "本地保留", "结果", "错误/说明"]];
  const id = job?.jobId || job?.operationId || "";
  for (const item of job?.results || []) {
    const state = item.deleted === true
      ? "已删除"
      : ["APPLE_BATCH_DEFERRED", "BATCH_DELETE_NOT_STARTED"].includes(item.code)
        ? "未执行"
        : "待核查";
    const retained = item.localRetained === true ? "已保留" : item.deleted === true ? "已删除" : "未知";
    rows.push([id, item.address || `ID ${item.id || ""}`, state, retained, item.code || "", item.message || ""]);
  }
  return rows.map((row) => row.map(csvCell).join(",")).join("\r\n") + "\r\n";
}
