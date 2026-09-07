[简体中文](#简体中文) · [English](#english)

> **v0.2 系列代号：Wind Rose**<br>
> **v0.2 series codename: Wind Rose**

## 简体中文

### v0.2.2 主要变化（相对 v0.2.1）

本次更新重点加入 **Tailscale 出站**：Mac 本机与通过 OpenSurge 接入的授权设备，可以访问 Tailnet 资源，并按需使用远端 Exit Node 作为公网出口。

- **Tailscale / Headscale 托管出站**：在“代理与规则源”中配置独立节点，通过 MagicDNS、指定节点地址或明确接受的远端子网访问私网服务。可分别授权 Mac 本机、指定设备或全部已注册设备；节点身份在重启、重载和暂时停用后保留。
- **Exit Node 融入现有分流**：明确配置 Exit Node 后，可以在 Mac 全局出口、设备出口，以及订阅或附加配置中的手动策略组里选择它。Tailnet 资源访问与公网出口分别配置，便于让不同设备和业务使用合适的路径。
- **更方便的 Tailscale 设置与连接**：可从本机 Tailscale App 发现节点、MagicDNS、远端子网和 Exit Node，确认后填入设置；发现结果可缓存，本机 App 断开后仍能继续配置。新增子网路由冲突提示，并在网关启动、重载或核心恢复后主动预热连接。
- **全局附加配置与停止态策略预览**：无需先导入订阅，也可以独立添加节点、策略组与规则。网关停止时就能预览合成后的策略、选择节点和检测延迟；从 Web GUI 启动时会校验并应用同一份候选配置。
- **失效出口不再阻塞启动**：切换来源或更新订阅后，设备当前默认出口不存在时暂时跟随网关规则；规则集、分流模板或直接条件分流的当前选中出口或固定出口不存在时跳过该绑定。仅未选中的候选失效时保留当前出口；原始设置保留，出口恢复后可在下次启动或重载时重新应用。
- **中英文界面与操作进度**：Web GUI 和菜单栏 App 支持简体中文、英语及跟随系统语言；启动、停止和重载显示当前阶段及结果，Tailscale 预热不再让操作一直等待完整探测响应。
- **修复 Mac 本机 IPv6 模式范围**：精确识别系统 TUN 中的本机 IPv6 流量，使规则／全局／直连模式正确作用于该流量，同时保持下游设备策略独立。

当前 Tailscale 集成用于出站访问，不向 Tailnet 发布 OpenSurge 的本地 LAN。Mac 本机与下游终端访问远端子网服务、下游终端使用 Exit Node，均已记录实际 HTTPS 成功结果；这些功能验证不代表全部网络环境、UDP/QUIC 或长期稳定性均已覆盖。

### 选择安装包

| Mac 类型 | 安装包 | 最低系统 |
| --- | --- | --- |
| Apple Silicon（M1 及更新芯片） | `arm64-unsigned.pkg` | macOS 13+ |
| Intel Mac | `x86_64-unsigned.pkg` | macOS 13+ |

> 安装包未进行 Developer ID 签名或 notarization。正式 Release 会同时提供 `SHA256SUMS` 和 GitHub build provenance，供下载后核验。

### 安装

1. 下载与你的 Mac 芯片匹配的安装包。
2. 双击安装包。如果 macOS 阻止打开，请进入**系统设置 → 隐私与安全性**，选择**仍要打开**并完成身份验证，然后重新打开安装包。
3. 安装完成后，从 `/Applications` 打开 **OpenSurge**。

安装完成后，网关默认保持停止；只有在 OpenSurge 控制面中明确操作后才会启动。

<details>
<summary>可选：校验下载文件</summary>

下载 `SHA256SUMS`，运行 `shasum -a 256 安装包名称`，并与文件中的对应记录比较。

也可以使用 GitHub CLI 核对安装包的构建来源：

```sh
gh attestation verify OpenSurge-for-Mac-*-arm64-unsigned.pkg \
  -R YTwsy/OpenSurge-for-Mac
```

Intel 安装包请将命令中的 `arm64` 替换为 `x86_64`。

</details>

### 许可证

OpenSurge 自有代码采用 `GPL-3.0-only`。第三方许可证、声明与准确的对应源码链接会安装到：

`/Library/Application Support/OpenSurge/share/licenses/`

- OpenSurge-patched Mihomo `1.19.30-opensurge.1` 的上游基线源码：<https://github.com/MetaCubeX/mihomo/tree/ac017cdd246ce8bd547653d927e7bf77d7ee73d5>
- dnsmasq 2.93 源码：<https://thekelleys.org.uk/dnsmasq/dnsmasq-2.93.tar.gz>

---

## English

### v0.2.2 highlights since v0.2.1

This release introduces **Tailscale outbound access**: the Mac and authorized devices connected through OpenSurge can reach Tailnet resources and optionally use a remote Exit Node for internet access.

- **Managed Tailscale / Headscale outbound:** Configure a separate node on the Sources page to reach private services through MagicDNS, specific peer addresses, or explicitly accepted remote subnets. Grant Tailnet access to the Mac, selected devices, or all registered devices. The node identity persists across restarts, reloads, and temporary disablement.
- **Exit Nodes in existing routing policies:** After configuring an Exit Node, select it from the Mac global outlet, device outlets, or manual policy groups in imported profiles and Global Extension. Tailnet resource access and internet egress are configured separately, so devices and services can use the appropriate path.
- **Easier Tailscale setup and connection:** Discover peers, MagicDNS, remote subnets, and Exit Nodes from the local Tailscale app, then review the suggestions before applying them. Cached discovery remains available after that app disconnects. Subnet-route conflict messages guide setup, and OpenSurge initiates connection warm-up after gateway startup, reload, or core recovery.
- **Global Extension and policy preview while stopped:** Add proxies, policy groups, and rules without importing a subscription first. Preview the composed policies, select nodes, and test latency while the gateway is stopped. Starting from the Web GUI validates and applies the same candidate configuration.
- **Missing outlets no longer block startup:** If a source change or subscription update removes a device's selected default outlet, it temporarily follows gateway rules. A device route bound to a rule set, routing template, or direct match condition is skipped when its selected or fixed outlet disappears. Missing unselected candidates do not change the current outlet. Original settings are retained, and restored outlets can be reapplied on the next start or reload.
- **Chinese and English UI with operation progress:** The Web GUI and menu bar app support Simplified Chinese, English, and the system language preference. Startup, shutdown, and reload show their current stage and result. Tailscale warm-up no longer holds the operation open while waiting for the full probe response.
- **Correct IPv6 scope for Mac routing modes:** Local IPv6 traffic entering the system TUN is identified precisely, allowing Rule, Global, and Direct modes to apply while keeping downstream device policies independent.

The Tailscale integration provides outbound access and does not advertise OpenSurge's local LAN to the Tailnet. Successful HTTPS requests have been recorded for Mac and downstream access to remote subnet services, and for downstream internet access through an Exit Node. These functional checks do not establish coverage of every network environment, UDP/QUIC, or long-term stability.

### Choose a package

| Mac | Package | Minimum system |
| --- | --- | --- |
| Apple Silicon (M1 or newer) | `arm64-unsigned.pkg` | macOS 13+ |
| Intel Mac | `x86_64-unsigned.pkg` | macOS 13+ |

> The installers are not Developer ID signed or notarized. The stable Release will also provide `SHA256SUMS` and GitHub build provenance for post-download verification.

### Install

1. Download the package matching your Mac.
2. Double-click the package. If macOS blocks it, open **System Settings → Privacy & Security**, choose **Open Anyway**, authenticate, and reopen the package.
3. After installation, open **OpenSurge** from `/Applications`.

The gateway remains stopped after installation and starts only when explicitly requested from the OpenSurge control plane.

<details>
<summary>Optional: verify the download</summary>

Download `SHA256SUMS`, run `shasum -a 256 PACKAGE_NAME`, and compare the result with the corresponding entry.

You can also verify the package's GitHub build provenance:

```sh
gh attestation verify OpenSurge-for-Mac-*-arm64-unsigned.pkg \
  -R YTwsy/OpenSurge-for-Mac
```

For the Intel package, replace `arm64` with `x86_64`.

</details>

### License

OpenSurge original code is licensed under `GPL-3.0-only`. Third-party license texts, notices, and exact corresponding-source links are installed under:

`/Library/Application Support/OpenSurge/share/licenses/`

- Upstream baseline source for OpenSurge-patched Mihomo `1.19.30-opensurge.1`: <https://github.com/MetaCubeX/mihomo/tree/ac017cdd246ce8bd547653d927e7bf77d7ee73d5>
- dnsmasq 2.93 source: <https://thekelleys.org.uk/dnsmasq/dnsmasq-2.93.tar.gz>
