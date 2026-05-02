#!/bin/bash
# Generate the rules ConfigMap from the rules/ directory.
# Run from the project root:
#   ./k8s/generate-rules-configmap.sh > k8s/configmap-rules.yaml

set -euo pipefail

RULES_DIR="$(cd "$(dirname "$0")/.." && pwd)/rules"

cat <<EOF
apiVersion: v1
kind: ConfigMap
metadata:
  name: email-automation-rules
  namespace: email-automation
  labels:
    app.kubernetes.io/name: email-automation
    app.kubernetes.io/component: rules
data:
EOF

for f in "$RULES_DIR"/*.lua; do
  name="$(basename "$f")"
  echo "  ${name}: |"
  sed 's/^/    /' "$f"
done
