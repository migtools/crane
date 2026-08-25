#!/usr/bin/env bash

set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  verify-build-origin.sh --repo <owner/name> --branch <branch> [--remote] [binary]
  <binary> version | verify-build-origin.sh --repo <owner/name> --branch <branch> [--remote]

Validates the build SHA embedded in `crane version` output against the expected
repository branch.

Arguments:
  binary              Path to a built crane binary. If omitted, version output is
                      read from stdin.

Options:
  --repo OWNER/NAME   Expected GitHub repository, for example migtools/crane
  --branch NAME       Expected branch, for example release-0.10
  --remote            Verify against GitHub instead of the local git checkout
  -h, --help          Show this help text
EOF
}

die() {
  echo "ERROR: $*" >&2
  exit 1
}

have_command() {
  command -v "$1" >/dev/null 2>&1
}

is_utf8_locale() {
  local locale="${LC_ALL:-${LC_CTYPE:-${LANG:-}}}"
  [[ "${locale}" == *UTF-8* || "${locale}" == *utf8* || "${locale}" == *UTF8* ]]
}

init_output_styles() {
  if [[ -t 1 && "${TERM:-}" != "dumb" && -z "${NO_COLOR:-}" ]]; then
    COLOR_GREEN=$'\033[32m'
    COLOR_YELLOW=$'\033[33m'
    COLOR_RED=$'\033[31m'
    COLOR_RESET=$'\033[0m'
  else
    COLOR_GREEN=""
    COLOR_YELLOW=""
    COLOR_RED=""
    COLOR_RESET=""
  fi

  if [[ -t 1 && "${TERM:-}" != "dumb" ]] && is_utf8_locale; then
    ICON_GREEN='✅ '
    ICON_YELLOW='⚠️ '
    ICON_RED='❌ '
    return 0
  fi

  ICON_GREEN=""
  ICON_YELLOW=""
  ICON_RED=""
}

format_status() {
  local status="$1" color="" icon=""

  case "${status}" in
    GREEN)
      color="${COLOR_GREEN}"
      icon="${ICON_GREEN}"
      ;;
    YELLOW)
      color="${COLOR_YELLOW}"
      icon="${ICON_YELLOW}"
      ;;
    RED)
      color="${COLOR_RED}"
      icon="${ICON_RED}"
      ;;
  esac

  if [[ -n "${icon}" ]]; then
    printf '%s%s%s%s' "${icon}" "${color}" "${status}" "${COLOR_RESET}"
    return 0
  fi

  printf '%s%s%s' "${color}" "${status}" "${COLOR_RESET}"
}

require_value() {
  local flag="$1"
  local value="${2:-}"
  [[ -n "$value" ]] || die "missing value for ${flag}"
}

normalize_remote_url() {
  local url="$1"
  url="${url%.git}"
  printf '%s\n' "$url"
}

matches_repo() {
  local remote_url
  remote_url="$(normalize_remote_url "$1")"
  local repo="$2"

  case "$remote_url" in
    "https://github.com/${repo}" | \
    "http://github.com/${repo}" | \
    "ssh://git@github.com/${repo}" | \
    "git@github.com:${repo}")
      return 0
      ;;
  esac

  return 1
}

is_shallow_repository() {
  [[ "$(git rev-parse --is-shallow-repository 2>/dev/null || printf 'false')" == "true" ]]
}

read_version_output() {
  if [[ -n "${BINARY_PATH}" ]]; then
    [[ -f "${BINARY_PATH}" ]] || die "binary not found: ${BINARY_PATH}"
    [[ -x "${BINARY_PATH}" ]] || die "binary is not executable: ${BINARY_PATH}"
    "${BINARY_PATH}" version
    return 0
  fi

  if [[ -t 0 ]]; then
    die "provide a binary path or pipe version output on stdin"
  fi

  local stdin_data
  stdin_data="$(</dev/stdin)"
  [[ -n "${stdin_data}" ]] || die "stdin did not contain version output"
  printf '%s\n' "${stdin_data}"
}

parse_version_field() {
  local label="$1"
  local input="$2"

  awk -v label="${label}" '
    $1 == label ":" {
      print $2
      exit
    }
  ' <<<"${input}"
}

parse_version_output() {
  CRANE_VERSION="$(parse_version_field "Version" "${VERSION_OUTPUT}")"
  BUILD_SHA="$(parse_version_field "SHA" "${VERSION_OUTPUT}")"

  [[ -n "${CRANE_VERSION}" ]] || die "could not parse Version from version output"
  [[ -n "${BUILD_SHA}" ]] || die "could not parse SHA from version output"
  [[ "${BUILD_SHA}" =~ ^[0-9a-fA-F]{7,40}$ ]] || die "parsed SHA is not a valid git commit: ${BUILD_SHA}"
}

print_report_header() {
  printf 'Binary:   %s\n' "${INPUT_SOURCE}"
  printf 'Version:  %s\n' "${CRANE_VERSION}"
  printf 'SHA:      %s\n' "${BUILD_SHA}"
  printf 'Repo:     %s\n' "${REPO}"
  printf 'Branch:   %s\n' "${BRANCH}"
  printf 'Mode:     %s\n' "${MODE}"
  printf '\n'
}

report_green() {
  print_report_header
  printf '%s - SHA %s is the latest commit on %s\n' "$(format_status "GREEN")" "${BUILD_SHA}" "${BRANCH}"
}

report_yellow() {
  local commits_behind="$1"
  local head_sha="$2"

  print_report_header
  printf '%s - SHA %s exists on %s but is %s commit(s) behind HEAD (%s)\n' \
    "$(format_status "YELLOW")" "${BUILD_SHA}" "${BRANCH}" "${commits_behind}" "${head_sha}"
}

report_red() {
  print_report_header
  printf '%s - SHA %s was not found on branch %s\n' "$(format_status "RED")" "${BUILD_SHA}" "${BRANCH}"
}

resolve_local_branch_ref() {
  git rev-parse --is-inside-work-tree >/dev/null 2>&1 || die "local mode requires running inside a git repository"

  local origin_url
  origin_url="$(git remote get-url origin 2>/dev/null || true)"
  [[ -n "${origin_url}" ]] || die "local mode requires an origin remote; use --remote if you do not have a local clone"
  matches_repo "${origin_url}" "${REPO}" || die "origin remote does not match ${REPO}; use --remote or run inside a clone of that repository"

  if ! git fetch --quiet origin "${BRANCH}"; then
    die "failed to fetch origin/${BRANCH}"
  fi

  if git rev-parse --verify "refs/remotes/origin/${BRANCH}" >/dev/null 2>&1; then
    printf 'refs/remotes/origin/%s\n' "${BRANCH}"
    return 0
  fi

  if git rev-parse --verify "refs/heads/${BRANCH}" >/dev/null 2>&1; then
    printf 'refs/heads/%s\n' "${BRANCH}"
    return 0
  fi

  die "branch ${BRANCH} was not found locally after fetching origin"
}

run_local_check() {
  local branch_ref head_sha commits_behind resolved_build_sha
  branch_ref="$(resolve_local_branch_ref)"
  head_sha="$(git rev-parse "${branch_ref}")"

  if ! resolved_build_sha="$(git rev-parse "${BUILD_SHA}^{commit}" 2>/dev/null)"; then
    if is_shallow_repository; then
      die "local repository history is too shallow to validate ${BUILD_SHA}; fetch more history or use --remote"
    fi
    report_red
    return 1
  fi

  if ! git merge-base --is-ancestor "${resolved_build_sha}" "${branch_ref}"; then
    if is_shallow_repository; then
      die "local repository history is too shallow to validate ${BUILD_SHA}; fetch more history or use --remote"
    fi
    report_red
    return 1
  fi

  if [[ "${resolved_build_sha}" == "${head_sha}" ]]; then
    report_green
    return 0
  fi

  commits_behind="$(git rev-list --count "${resolved_build_sha}..${branch_ref}")"
  report_yellow "${commits_behind}" "${head_sha}"
  return 1
}

github_compare_json() {
  if have_command gh; then
    if gh api -H "Accept: application/vnd.github+json" "repos/${REPO}/compare/${BUILD_SHA}...${BRANCH}"; then
      return 0
    fi
  fi

  have_command curl || die "remote mode requires gh or curl"
  have_command python3 || die "remote mode requires python3 when gh is unavailable"

  local encoded_branch
  encoded_branch="$(python3 -c 'import sys, urllib.parse; print(urllib.parse.quote(sys.argv[1], safe=""))' "${BRANCH}")"

  curl -fsSL \
    -H "Accept: application/vnd.github+json" \
    "https://api.github.com/repos/${REPO}/compare/${BUILD_SHA}...${encoded_branch}"
}

parse_compare_result() {
  python3 -c '
import json
import sys

data = json.load(sys.stdin)
print(data.get("status", ""))
print(data.get("ahead_by", 0))
' <<<"${1}"
}

resolve_remote_branch_head() {
  local line
  line="$(git ls-remote --exit-code "https://github.com/${REPO}.git" "refs/heads/${BRANCH}" 2>/dev/null || true)"
  [[ -n "${line}" ]] || die "branch ${BRANCH} was not found in ${REPO}"
  awk '{print $1}' <<<"${line}"
}

run_remote_check() {
  have_command git || die "remote mode requires git"
  have_command python3 || die "remote mode requires python3"

  local head_sha compare_json compare_status ahead_by compare_fields
  head_sha="$(resolve_remote_branch_head)"

  if ! compare_json="$(github_compare_json 2>/dev/null)"; then
    die "failed to query GitHub compare API for ${REPO}#${BRANCH}"
  fi

  compare_fields="$(parse_compare_result "${compare_json}")"
  compare_status="$(awk 'NR == 1 { print; exit }' <<<"${compare_fields}")"
  ahead_by="$(awk 'NR == 2 { print; exit }' <<<"${compare_fields}")"
  ahead_by="${ahead_by:-0}"

  case "${compare_status}" in
    identical)
      report_green
      return 0
      ;;
    ahead)
      report_yellow "${ahead_by}" "${head_sha}"
      return 1
      ;;
    *)
      report_red
      return 1
      ;;
  esac
}

REPO=""
BRANCH=""
REMOTE_MODE=false
BINARY_PATH=""

init_output_styles

while [[ $# -gt 0 ]]; do
  case "$1" in
    --repo)
      require_value "$1" "${2:-}"
      REPO="$2"
      shift 2
      ;;
    --branch)
      require_value "$1" "${2:-}"
      BRANCH="$2"
      shift 2
      ;;
    --remote)
      REMOTE_MODE=true
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    --)
      shift
      while [[ $# -gt 0 ]]; do
        [[ -z "${BINARY_PATH}" ]] || die "only one binary path may be provided"
        BINARY_PATH="$1"
        shift
      done
      break
      ;;
    -*)
      die "unknown option: $1"
      ;;
    *)
      [[ -z "${BINARY_PATH}" ]] || die "only one binary path may be provided"
      BINARY_PATH="$1"
      shift
      ;;
  esac
done

[[ -n "${REPO}" ]] || die "--repo is required"
[[ -n "${BRANCH}" ]] || die "--branch is required"

VERSION_OUTPUT="$(read_version_output)"
parse_version_output

if [[ -n "${BINARY_PATH}" ]]; then
  INPUT_SOURCE="${BINARY_PATH}"
else
  INPUT_SOURCE="stdin"
fi

if [[ "${REMOTE_MODE}" == "true" ]]; then
  MODE="remote github"
  run_remote_check
else
  MODE="local git"
  run_local_check
fi
