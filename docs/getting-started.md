# dsh-mygo 测试版：开始使用

此测试版面向 Windows x64，通过公开 GitHub Releases 分发。应用安装器只包含本体；Node 与 DeepSeek Harness 作为独立的运行时 bundle 分发，也可以在已安装 Node 24+ 与精确版本 DSH 的机器上直接复用现有 DSH。无需安装 Go、Git 或命令行工具。

## 安装与启动

1. 从 GitHub Release 的 Assets 下载 `dsh-mygo-<版本>-windows-x64-setup.exe`；需要自带后端时再下载对应的 `dsh-mygo-runtime-*.tgz`。
2. 运行安装器，完成后从开始菜单打开 **dsh-mygo**。安装面向当前 Windows 用户。
3. 首次启动进入 **Set up DeepSeek Harness** 引导：
   - **使用已安装的 DSH**：应用会检测 `PATH` 上的 `dsh` 与 `node`。DSH 版本必须精确匹配（当前 `0.2.1-rc.2`），Node 需要 24+；也可以手动选择 `dsh` 命令。
   - **安装 runtime bundle**：点击 **Download latest runtime** 从 GitHub Releases 下载，或使用 **Choose .tgz file…** 导入离线文件。下载/解压显示进度，可取消。
4. 选择成功后应用会启动 Host；之后每次启动都使用这个选择。此过程不需要模型 API key。

测试包采用自签名证书，Windows 可能显示 Unknown publisher 或 SmartScreen 提示。先确认文件来自项目维护者提供的 Release；可以使用 PowerShell `Get-FileHash <安装包路径> -Algorithm SHA256` 与 `SHA256SUMS.txt` 核对。随包提供的 `dsh-mygo-signing.cer` 只包含公开证书，不需要为了试用而导入系统信任。

## 完成第一轮对话

1. 在输入框下方点击项目目录（**Choose project folder / 选择项目文件夹**），选择要处理的项目。选择后会新建该文件夹下的会话，原会话和草稿保留。确认显示的路径正确；也可以点击 **Use this folder / 使用此文件夹** 使用当前显示的目录。
2. 打开 **Preferences → Connection / 设置 → 连接**，输入自己的 API key 并点击 **Save key / 保存密钥**。凭据只写入本机 DSH，不会回显；已配置状态并不代表通过认证。如果已通过环境配置提供凭据，可以直接使用。
3. 关闭连接设置，在输入框发送一条简单消息，例如「介绍一下这个项目的结构」。在线对话需要有效的 DeepSeek API key、账户余额和网络连接，按自己的账户计费。
4. 当工具请求审批时检查内容后作出选择；需要中断时点击 **Stop / 停止**。默认权限遵循当前会话设置，可从输入框下方的独立权限按钮调整。

可以点击连接设置中的 **Get API key / 获取 API key** 打开 [DeepSeek 开放平台](https://platform.deepseek.com/)；也可参考 [官方 API 文档](https://api-docs.deepseek.com/api/deepseek-api/)。API key 使用自己的账户获取，本应用不提供共享凭据或代充值。

## 运行时管理

打开 **Preferences → Runtime**：

- **Current runtime** 显示当前后端的类型、DSH、Node、Host bundle 版本与路径，可复制路径。
- **Installed runtimes** 可切换（Use）或移除（Remove）；当前项不能移除，移除外部 DSH 只从列表遗忘，不删除用户文件。
- **Add runtime** 可重新检测已安装 DSH、从 `.tgz` 安装，或检查 GitHub 上的可用版本。下载/安装只加入列表，准备好后再 Use。
- 切换会重启 Host 并恢复当前会话；失败时自动恢复之前的运行时。安装/下载期间不能关闭设置，可取消。
- 离线环境：把 runtime `.tgz` 复制到本机，用 **Install from file…** 导入。
- Host 无法启动时，恢复对话框提供 **Change runtime…**，可直接更换后端而不必重启应用。

## 日常使用

- **更换项目**：点击输入框下方的项目目录选择文件夹；选择不同目录会新建会话，原会话和草稿保留。侧栏按项目分组，每个项目旁的加号可在该目录新建。已有会话的工作目录保持不变。
- **切换模型**：点击输入框下方模型按钮，在快捷列表中搜索并选择，直接应用到当前会话；推理强度可在同一列表切换。More settings 中的 Save selection as default 影响以后新建或冷恢复的会话。
- **找回对话**：宽窗口使用左侧会话列表；窄窗口点击工具栏 Sessions。应用重开后恢复最近会话和草稿。
- **浏览历史**：向上滚动接近顶部会自动加载更早的完整回合；输入框上方的圆形向下箭头可返回最新位置。切换会话的加载提示显示在中间对话区，侧栏保持固定。
- **查看子代理**：点击工具栏 Subagents 查看任务、消息、工具输出和结束原因，通过 Refresh 更新提交结果。
- **中文和主题**：点击工具栏 Preferences，选择简体中文、浅色/深色或系统主题并保存。
- **Host 错误**：使用恢复对话框的 Restart 重启，Change runtime… 更换后端，View logs 打开日志目录。反馈问题时附版本、复现步骤和截图，分享前检查日志中是否含自己的项目内容。

## 数据、更新与卸载

会话、设置和凭据默认保存在当前用户的 `~/.dsh` 下；草稿和界面偏好保存在本机应用数据目录；runtime 安装在本机缓存目录（Windows `%LocalAppData%` 下的 dsh-mygo 缓存）。API key 由 DSH 本地管理，并非加密的系统密钥库。不要把这些目录作为反馈附件或复制到公共位置。

此测试版没有自动更新。收到新版本后，退出应用，再运行新的安装器覆盖安装；应用与 runtime 可独立更新，已安装的 runtime 与 `~/.dsh` 数据不会被安装器覆盖。卸载不会删除 `~/.dsh` 下的会话和凭据，也不会自动删除已下载的 runtime；可在设置中先移除，或卸载后手动清理缓存目录。

首发范围为 Windows x64。干净 Windows VM 和本人的付费模型回合仍需试用验证；macOS、自动更新和后台跨会话交互在后续版本迭代。
