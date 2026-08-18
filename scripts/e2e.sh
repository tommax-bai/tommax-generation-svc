#!/usr/bin/env bash
# 纵向切片 E2E：提交文生图 → 轮询 → 校验产物可下载。
# 前置：tommax-infra compose 已启动；model-adapter-svc 与 generation-svc 已运行。
set -euo pipefail

API="${API:-http://127.0.0.1:8080}"
USER_HEADER="X-Dev-User: e2e-user"
REQ_ID="e2e-$(date +%s)"

echo "==> submit"
SUBMIT=$(curl -sf -X POST "$API/v1/generations" \
  -H 'Content-Type: application/json' -H "$USER_HEADER" \
  -d "{\"taskType\":\"image.text2img\",\"modelKey\":\"mock-image-v1\",\"prompt\":\"e2e smoke\",\"params\":{\"n\":\"1\"},\"requestId\":\"$REQ_ID\"}")
TASK_ID=$(echo "$SUBMIT" | python3 -c 'import json,sys; print(json.load(sys.stdin)["data"]["id"])')
echo "    task=$TASK_ID"

echo "==> poll"
for i in $(seq 1 30); do
  sleep 1.5
  RESP=$(curl -sf "$API/v1/generations/$TASK_ID" -H "$USER_HEADER")
  STATUS=$(echo "$RESP" | python3 -c 'import json,sys; print(json.load(sys.stdin)["data"]["status"])')
  echo "    #$i $STATUS"
  [ "$STATUS" = "SUCCEEDED" ] && break
  if [ "$STATUS" = "FAILED" ] || [ "$STATUS" = "CANCELED" ]; then
    echo "E2E FAILED: terminal status $STATUS"; echo "$RESP"; exit 1
  fi
done
[ "$STATUS" = "SUCCEEDED" ] || { echo "E2E FAILED: timeout"; exit 1; }

echo "==> verify output"
URL=$(echo "$RESP" | python3 -c 'import json,sys; print(json.load(sys.stdin)["data"]["outputs"][0]["assetUrl"])')
curl -sf -o /tmp/e2e-output.bin "$URL"
file /tmp/e2e-output.bin | grep -q "PNG image" || { echo "E2E FAILED: output is not PNG"; exit 1; }
echo "E2E OK: $URL"
