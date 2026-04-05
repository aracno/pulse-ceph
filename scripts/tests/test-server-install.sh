#!/usr/bin/env bash
#
# Smoke tests for the top-level server installer.

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
INSTALL_SCRIPT="${ROOT_DIR}/install.sh"

if [[ ! -f "${INSTALL_SCRIPT}" ]]; then
  echo "install.sh not found at ${INSTALL_SCRIPT}" >&2
  exit 1
fi

failures=0

assert_success() {
  local desc="$1"
  shift
  if "$@"; then
    echo "[PASS] ${desc}"
    return 0
  else
    echo "[FAIL] ${desc}" >&2
    ((failures++))
    return 1
  fi
}

load_installer() {
  # shellcheck disable=SC1090
  source "${INSTALL_SCRIPT}"
  trap - EXIT
}

test_infer_release_from_archive_name_supports_prerelease() {
  (
    load_installer
    local version
    version="$(infer_release_from_archive_name "/tmp/pulse-v5.1.27-rc.1-linux-arm64.tar.gz")"
    [[ "${version}" == "v5.1.27-rc.1" ]]
  )
}

test_download_pulse_installs_from_local_archive_without_network() {
  (
    load_installer

    local tmpdir archive_root archive_path
    tmpdir="$(mktemp -d)"
    archive_root="${tmpdir}/archive"
    archive_path="${tmpdir}/pulse-v5.1.99-linux-amd64.tar.gz"

    mkdir -p "${archive_root}/bin"
    cat > "${archive_root}/bin/pulse" <<'EOF'
#!/usr/bin/env bash
echo "v5.1.99"
EOF
    chmod +x "${archive_root}/bin/pulse"
    printf 'v5.1.99\n' > "${archive_root}/VERSION"
    tar -czf "${archive_path}" -C "${archive_root}" .

    INSTALL_DIR="${tmpdir}/opt/pulse"
    CONFIG_DIR="${tmpdir}/etc/pulse"
    BUILD_FROM_SOURCE=false
    SKIP_DOWNLOAD=false
    ARCHIVE_OVERRIDE="${archive_path}"
    FORCE_VERSION=""
    FORCE_CHANNEL=""
    UPDATE_CHANNEL="stable"
    LATEST_RELEASE=""
    STOPPED_PULSE_SERVICE=""

    mkdir -p "${INSTALL_DIR}/bin" "${CONFIG_DIR}"

    detect_service_name() { echo "pulse"; }
    stop_pulse_service_for_update() { return 0; }
    restore_selinux_contexts() { :; }
    install_additional_agent_binaries() { return 0; }
    deploy_agent_scripts() { return 0; }
    validate_pulse_binary_architecture() { return 0; }
    chown() { :; }
    ln() { :; }
    curl() { echo "unexpected curl call" >&2; return 99; }
    wget() { echo "unexpected wget call" >&2; return 99; }

    download_pulse

    [[ -x "${INSTALL_DIR}/bin/pulse" ]]
    [[ "$("${INSTALL_DIR}/bin/pulse" --version)" == "v5.1.99" ]]
    [[ "${LATEST_RELEASE}" == "v5.1.99" ]]
  )
}

test_prefetch_pulse_archive_for_container_sets_output_var() {
  (
    load_installer

    local archive_path=""
    LATEST_RELEASE="v5.1.42"

    resolve_target_release() { :; }
    download_release_archive() {
      printf 'test archive\n' > "$3"
      return 0
    }
    uname() { echo "x86_64"; }

    prefetch_pulse_archive_for_container archive_path

    [[ "${archive_path}" == /tmp/pulse-v5.1.42-amd64-lxc-*.tar.gz ]]
    [[ -f "${archive_path}" ]]
    rm -f "${archive_path}"
  )
}

test_install_pulse_archive_rejects_mismatched_arch_without_replacing_existing_binary() {
  (
    load_installer

    local tmpdir archive_root archive_path
    tmpdir="$(mktemp -d)"
    archive_root="${tmpdir}/archive"
    archive_path="${tmpdir}/pulse-v5.1.99-linux-arm64.tar.gz"

    mkdir -p "${archive_root}/bin"
    cat > "${archive_root}/bin/pulse" <<'EOF'
#!/usr/bin/env bash
echo "v5.1.99"
EOF
    chmod +x "${archive_root}/bin/pulse"
    tar -czf "${archive_path}" -C "${archive_root}" .

    INSTALL_DIR="${tmpdir}/opt/pulse"
    mkdir -p "${INSTALL_DIR}/bin"
    cat > "${INSTALL_DIR}/bin/pulse" <<'EOF'
#!/usr/bin/env bash
echo "v5.1.10"
EOF
    chmod +x "${INSTALL_DIR}/bin/pulse"

    validate_pulse_binary_architecture() { return 1; }

    if install_pulse_archive "${archive_path}" "v5.1.99"; then
      echo "install_pulse_archive unexpectedly succeeded on mismatched archive" >&2
      rm -rf "${tmpdir}"
      return 1
    fi

    [[ "$("${INSTALL_DIR}/bin/pulse" --version)" == "v5.1.10" ]]
    [[ ! -e "${INSTALL_DIR}/bin/pulse.old" ]]
    rm -rf "${tmpdir}"
  )
}

test_parse_args_rejects_archive_with_source() {
  local tmpdir output_file
  tmpdir="$(mktemp -d)"
  output_file="${tmpdir}/output.txt"

  if bash "${INSTALL_SCRIPT}" --source --archive /tmp/pulse.tar.gz >"${output_file}" 2>&1; then
    echo "installer unexpectedly accepted --archive with --source" >&2
    rm -rf "${tmpdir}"
    return 1
  fi

  if ! grep -q -- "--archive cannot be used with --source" "${output_file}"; then
    echo "expected archive/source validation message" >&2
    cat "${output_file}" >&2
    rm -rf "${tmpdir}"
    return 1
  fi

  rm -rf "${tmpdir}"
  return 0
}

main() {
  assert_success "infer_release_from_archive_name parses prerelease tarballs" test_infer_release_from_archive_name_supports_prerelease
  assert_success "download_pulse installs from local archive without network" test_download_pulse_installs_from_local_archive_without_network
  assert_success "prefetch helper writes archive path via output variable" test_prefetch_pulse_archive_for_container_sets_output_var
  assert_success "wrong-arch archives fail before replacing the installed binary" test_install_pulse_archive_rejects_mismatched_arch_without_replacing_existing_binary
  assert_success "parse_args rejects archive with source builds" test_parse_args_rejects_archive_with_source

  if (( failures > 0 )); then
    echo "Total failures: ${failures}" >&2
    return 1
  fi

  echo "All server installer smoke tests passed."
}

main "$@"
