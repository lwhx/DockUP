# Changelog

## v0.7.15

- 修复 Telegram 按钮偶发点击无响应：回调轮询失败现在会记录限频错误并使用可取消的指数退避，轮询异常退出后自动重建。
- 修复 Telegram API 错误被静默忽略的问题；过期或未知按钮现在会给出明确提示，并增加回调处理日志和回归测试。

## v0.7.14

- 修复 Parrot 等镜像在 `latest` 指向同一源码 revision 的 `main` 构建时被误报更新的问题。
- 优化 GHCR 版本识别：当 digest 无法直接匹配数字版本 tag 时，会按 OCI revision 反查同 commit 的语义化版本，避免只显示短镜像 ID。

## v0.7.13

- 修复容器实际更新完成后，Telegram 消息仍可能停留在“正在更新”的问题：成功/失败消息编辑现在使用独立短超时上下文，并记录编辑失败日志。
- 更新完成后新增明确日志，便于区分“容器更新卡住”和“Telegram 消息编辑失败”。

## v0.7.12

- 修复点击“更新”后 Telegram 消息可能一直停留在“正在更新”的问题：更新任务不再继承 callback polling 请求上下文，避免 getUpdates 返回后后台更新被取消。
- 适配较慢的 `latest` 镜像更新流程，容器实际更新完成后能继续编辑为成功/失败结果。

## v0.7.11

- 修复反向连接 Agent 更新耗时较长的远程容器时，中心端可能收不到完成回执，导致 Telegram 消息一直停留在“正在更新”的问题。
- 反向连接 Agent 现在会沿用配置的 `UPDATE_TIMEOUT` 处理远程请求，适配较大的镜像拉取和容器重建。

## v0.7.8

- 修复离线远程 VPS 详情页无法直接删除服务器的问题。

## v0.7.6

- 优化 Docker 版本识别：忽略 `latest`、`main`、`nightly` 等非数字版本标签。
- 支持从 Node 项目镜像内的 `package.json` 读取版本号。
- 支持识别 `TGBot_RSS` 二进制版本。

## v0.7.5

- 修复无版本 Label 的 Docker Hub 镜像版本显示，例如 `xream/sub-store` 可显示 `2.24.1`。
- 优化 `latest` 镜像更新识别，减少有更新但页面显示不准确的问题。

## v0.7.4

- 支持 GHCR 版本 tag 反查。
- 优化 latest 镜像版本显示。

## v0.7.3

- 优化 Sub-Store 版本显示。
- 修复 latest 镜像更新检测。

## v0.7.2

- Fixed confirmed container updates failing when the target image was pulled during the update check but later removed by Docker cleanup before the user clicked update. DockUP now re-pulls and verifies the target image immediately before recreating the container.
- Fixed successful updates appearing stuck on “updating” while old image cleanup was still running. Old image cleanup now runs in the background after the new container has been recreated and started.

## v0.7.1

- Fixed Telegram commands and buttons becoming unresponsive while the startup update check is still running. The initial check now runs in the background so the bot can process polling updates immediately after boot.

## v0.7.0

- Fixed remote Agent self-updates on reverse-connected VPS nodes. The temporary self-update helper now shares the target Agent container network namespace, so it can preserve the reverse connection long enough to recreate and restart the Agent cleanly instead of leaving it stopped.

## v0.6.16

- Fixed update detection for Compose-managed containers whose Docker list image is reported as a `sha256:...` image ID. DockUP now resolves the original container image reference before pulling, so services such as `xream/sub-store` are detected correctly again.
- Applied the same image-reference fallback to local automatic checks, manual project checks, and remote Agent checks.

## v0.6.15

- Fixed remote Agent self-updates hanging batch updates. Agents now acknowledge self-update requests before starting the helper container, avoiding reverse-connection EOF stalls during “update all”.

## v0.6.14

- Improved Telegram button responsiveness: callbacks are handled asynchronously, so update scans no longer block normal button navigation.
- Made the local Docker project list faster by grouping projects from Docker list metadata instead of inspecting every container.
- Improved Docker detail version detection for `latest` images without version labels by using the original image reference plus digest lookup, so images such as `xream/sub-store` can resolve to concrete tags like `2.23.21`.

## v0.6.13

- Fixed Docker detail version detection for images inspected by image ID (`sha256:...`). DockUP now prefers semantic version labels like `v0.6.10` instead of treating the sha256 ID as a tag.

## v0.6.12

- Improved version display: when no semantic version tag or image label is available, DockUP now shows only the short image ID instead of repeating the full sha-like digest plus the same short ID.

## v0.6.11

- Fixed remote reverse Agent self-updates. When an Agent is asked to update its own `dockup-agent` container, it now starts a self-update helper and returns immediately instead of stopping itself mid-request and making the center appear stuck.

## v0.6.10

- Fixed manual “check all” result buttons so remote updates are included in the per-item and batch update buttons, matching the reported update count.

## v0.6.9

- Rebuilt release for manual update verification. No runtime deployment is performed automatically; use DockUP to update local and remote agents manually.

## v0.6.8

- Changed manual “check all” to be a true fresh full scan. It no longer depends on pending/ignored notification state: every manual check lists all currently available updates and provides fresh per-item and batch update buttons on the result page.

## v0.6.7

- Fixed “check all” summaries when updates were already pending. The result now separates newly detected updates from existing pending update notifications and the batch update button includes all pending updates.

## v0.6.6

- Changed Docker publishing so `latest` is only moved by version tags. This keeps the running `latest` image labeled with the release version instead of `main`, making Docker detail pages show semantic versions consistently.

## v0.6.5

- Added a batch update button to the “check all” result when updates are found.
- Improved check summaries by separating non-pullable local images from real check failures and listing skipped local-only images.
- Fixed local manual project update checks to compare against the running container image ID, matching the automatic check behavior.

## v0.6.4

- Improved manual “check all” feedback to show how many local containers and remote VPS were checked, how many updates were found, and whether any checks failed.
- Added running image version display to Docker and remote Docker detail pages, e.g. `v0.6.4 (abcdef123456)` when image labels or semantic tags are available.

## v0.6.3

- Fixed update detection after manually checking `latest` images. DockUP now compares the running container image ID against the pulled tag image ID, so repeated checks still show an update until the container is actually recreated.

## v0.6.2

- Added manual update checks to remote VPS pages and remote project pages.
- Remote manual checks now reuse the existing update confirmation flow, so updates can be applied from the remote check result.

## v0.6.1

- Fixed preconfigured reverse Agents using `DOCKUP_AGENTS` with an empty URL, e.g. `id||Name|token`.
- Improved Telegram server onboarding so sending an IP address or Agent URL as the first reply directly generates the Agent install command with a sensible default name.

## v0.6.0

- Added a remote VPS removal flow in Telegram.
  - The bot shows a fixed Agent uninstall command for the user to run on the remote host.
  - The server removes the local record only after the Agent is offline.
  - A force-remove option is available for already-destroyed remote hosts.
- Added automatic `DOCKUP_DATA` backups before removing remote VPS records.
- Added Telegram command and callback authorization against `TG_CHAT_ID`.
- Improved common user-facing error messages for offline Agents, network timeouts, auth failures, missing images, Docker permission issues, and stale projects.
- Updated Chinese and English README documentation for the safer remote VPS deletion behavior.

## v0.5.0

- Added reverse Agent VPS management.
