#!/usr/bin/env bash
set -euo pipefail

archive_path="${1:-}"
binary_name="${RELEASE_ARCHIVE_VALIDATION_BINARY_NAME:-caam}"
out_json="${RELEASE_ARCHIVE_VALIDATION_OUT_JSON:-artifacts/test-audit/installed_validation_summary.json}"
out_md="${RELEASE_ARCHIVE_VALIDATION_OUT_MD:-artifacts/test-audit/installed_validation_summary.md}"
tmp_root="${RELEASE_ARCHIVE_VALIDATION_TMP_ROOT:-}"

if [[ -z "${archive_path}" ]]; then
  echo "usage: $0 /path/to/release-archive.{tar.gz,zip}" >&2
  exit 2
fi

if [[ ! -f "${archive_path}" ]]; then
  echo "release archive does not exist: ${archive_path}" >&2
  exit 2
fi

cleanup_tmp=0
if [[ -z "${tmp_root}" ]]; then
  tmp_root="$(mktemp -d)"
  cleanup_tmp=1
fi

cleanup() {
  if [[ "${cleanup_tmp}" == "1" ]]; then
    rm -rf "${tmp_root}"
  fi
}
trap cleanup EXIT

extract_dir="${tmp_root}/extract"
mkdir -p "${extract_dir}"

case "${archive_path}" in
  *.tar.gz)
    tar -xzf "${archive_path}" -C "${extract_dir}"
    ;;
  *.zip)
    if ! command -v unzip >/dev/null 2>&1; then
      echo "unzip is required to validate zip archives" >&2
      exit 2
    fi
    unzip -q "${archive_path}" -d "${extract_dir}"
    ;;
  *)
    echo "unsupported archive format: ${archive_path}" >&2
    exit 2
    ;;
esac

bin_path="$(find "${extract_dir}" -type f \( -name "${binary_name}" -o -name "${binary_name}.exe" \) | head -n 1)"
if [[ -z "${bin_path}" ]]; then
  echo "failed to find ${binary_name} binary in extracted archive" >&2
  find "${extract_dir}" -maxdepth 3 -type f >&2 || true
  exit 1
fi

if [[ ! -x "${bin_path}" ]]; then
  chmod +x "${bin_path}" || true
fi

INSTALLED_VALIDATION_PROVENANCE="release_artifact" \
INSTALLED_VALIDATION_ARTIFACT_PATH="${archive_path}" \
INSTALLED_VALIDATION_TMP_ROOT="${tmp_root}/installed-env" \
INSTALLED_VALIDATION_OUT_JSON="${out_json}" \
INSTALLED_VALIDATION_OUT_MD="${out_md}" \
./scripts/validate_installed_binary.sh "${bin_path}"
