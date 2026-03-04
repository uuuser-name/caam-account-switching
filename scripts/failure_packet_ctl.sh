#!/usr/bin/env bash
set -euo pipefail

REGISTRY_ROOT="${FAILURE_PACKET_REGISTRY:-artifacts/failure-packets-registry}"
INDEX_PATH="${REGISTRY_ROOT}/index.json"

usage() {
  cat <<'USAGE'
Usage:
  scripts/failure_packet_ctl.sh publish [packet_dir]
  scripts/failure_packet_ctl.sh latest
  scripts/failure_packet_ctl.sh fetch <packet_id> [dest_dir]
  scripts/failure_packet_ctl.sh list

Commands:
  publish   Publish a generated packet directory into local/CI registry index.
            packet_dir defaults to latest directory under artifacts/failure-packets/.
  latest    Print latest packet metadata as JSON.
  fetch     Copy one published packet directory (and archive if present) to dest_dir.
  list      Print all published packet metadata as JSON.
USAGE
}

ensure_registry() {
  mkdir -p "${REGISTRY_ROOT}"
  if [[ ! -f "${INDEX_PATH}" ]]; then
    echo "[]" > "${INDEX_PATH}"
  fi
}

latest_default_packet_dir() {
  find artifacts/failure-packets -mindepth 1 -maxdepth 1 -type d 2>/dev/null | sort | tail -1
}

publish_packet() {
  ensure_registry
  local packet_dir="${1:-$(latest_default_packet_dir)}"
  if [[ -z "${packet_dir}" || ! -d "${packet_dir}" ]]; then
    echo "No packet directory found to publish" >&2
    exit 1
  fi

  local packet_id
  packet_id="$(basename "${packet_dir}")"
  local dest_dir="${REGISTRY_ROOT}/${packet_id}"
  mkdir -p "${dest_dir}"
  cp -R "${packet_dir}/." "${dest_dir}/"

  local archive_src
  archive_src="$(dirname "${packet_dir}")/failure-packet-${packet_id}.tar.gz"
  local archive_dest=""
  if [[ -f "${archive_src}" ]]; then
    archive_dest="${REGISTRY_ROOT}/$(basename "${archive_src}")"
    cp "${archive_src}" "${archive_dest}"
  fi

  local summary_path="${dest_dir}/FAILURE_SUMMARY.md"
  local now_utc
  now_utc="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

  local tmp
  tmp="$(mktemp)"
  jq \
    --arg id "${packet_id}" \
    --arg timestamp "${now_utc}" \
    --arg dir "${dest_dir}" \
    --arg archive "${archive_dest}" \
    --arg summary "${summary_path}" \
    '. = ([.[] | select(.id != $id)] + [{id:$id,timestamp:$timestamp,dir:$dir,archive:$archive,summary:$summary}])' \
    "${INDEX_PATH}" > "${tmp}"
  mv "${tmp}" "${INDEX_PATH}"

  echo "Published packet:"
  echo "  id: ${packet_id}"
  echo "  dir: ${dest_dir}"
  if [[ -n "${archive_dest}" ]]; then
    echo "  archive: ${archive_dest}"
  fi
}

show_latest() {
  ensure_registry
  jq 'sort_by(.timestamp) | last // {}' "${INDEX_PATH}"
}

list_packets() {
  ensure_registry
  jq '.' "${INDEX_PATH}"
}

fetch_packet() {
  ensure_registry
  local packet_id="${1:-}"
  local dest_dir="${2:-.}"
  if [[ -z "${packet_id}" ]]; then
    echo "fetch requires <packet_id>" >&2
    exit 1
  fi

  local src_dir="${REGISTRY_ROOT}/${packet_id}"
  if [[ ! -d "${src_dir}" ]]; then
    echo "Packet not found: ${packet_id}" >&2
    exit 1
  fi

  mkdir -p "${dest_dir}/${packet_id}"
  cp -R "${src_dir}/." "${dest_dir}/${packet_id}/"

  local archive_path="${REGISTRY_ROOT}/failure-packet-${packet_id}.tar.gz"
  if [[ -f "${archive_path}" ]]; then
    cp "${archive_path}" "${dest_dir}/"
  fi

  echo "Fetched packet ${packet_id} into ${dest_dir}"
}

main() {
  local cmd="${1:-}"
  case "${cmd}" in
    publish)
      publish_packet "${2:-}"
      ;;
    latest)
      show_latest
      ;;
    list)
      list_packets
      ;;
    fetch)
      fetch_packet "${2:-}" "${3:-.}"
      ;;
    ""|-h|--help|help)
      usage
      ;;
    *)
      echo "Unknown command: ${cmd}" >&2
      usage
      exit 1
      ;;
  esac
}

main "$@"
