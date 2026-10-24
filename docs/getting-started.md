# dsh-mygo 测试版：开始使用

此测试版面向 Windows x64，通过私有 GitHub 仓库分发，需要有仓库访问权限。安装包已包含 Node 和 DeepSeek Harness，无需安装 Go、Node、Git 或命令行工具。

## 安装与启动

1. 从 GitHub Release 的 Assets 下载 `dsh-mygo-<版本>-windows-x64-setup.exe`。
2. 运行安装器，完成后从开始菜单打开 **dsh-mygo**。安装面向当前 Windows 用户。
3. 首次启动会在本机创建运行配置；此过程不需要模型 API key，也不下载开发依赖。

测试包采用自签名证书，Windows 可能显示 Unknown publisher 或 SmartScreen 提示。先确认文件来自项目维护者提供的 Release；可以使用 PowerShell `Get-FileHash <安装包路径> -Algorithm SHA256` 与 `SHA256SUMS.txt` 核对。随包提供的 `dsh-mygo-signing.cer` 只包含公开证书，不需要为了试用而导入系统信任。

## 完成第一轮对话

1. 在空会话点击 **Choose project folder / 选择项目文件夹**，选择要处理的项目。选择后会新建该文件夹下的会话，原会话和草稿保留。确认显示的路径正确；也可以点击 **Use this folder / 使用此文件夹** 使用当前显示的目录。
2. 点击 **Connect DeepSeek / 连接 DeepSeek**，输入自己的 API key 并点击 **Save key / 保存密钥**。凭据只写入本机 DSH，不会回显；已配置状态并不代表通过认证。如果已通过环境配置提供凭据，可以直接使用。
3. 关闭连接设置，在输入框发送一条简单消息，例如「介绍一下这个项目的结构」。在线对话需要有效的 DeepSeek API key、账户余额和网络连接，按自己的账户计费。
4. 当工具请求审批时检查内容后作出选择；需要中断时点击 **Stop / 停止**。默认权限遵循当前会话设置，可从输入框的模型/权限按钮调整。

可以点击连接设置中的 **Get API key / 获取 API key** 打开 [DeepSeek 开放平台](https://platform.deepseek.com/)；也可参考 [官方 API 文档](https://api-docs.deepseek.com/api/deepseek-api/)。API key 使用自己的账户获取，本应用不提供共享凭据或代充值。

## 日常使用

- **更换项目**：在空会话选择项目文件夹；有历史的会话先新建对话，再选择文件夹。已有会话的工作目录保持不变。
- **切换模型**：点击输入框下方模型按钮。Apply model 影响当前会话，Save selection as default 影响以后新建或冷恢复的会话。
- **找回对话**：宽窗口使用左侧会话列表；窄窗口点击工具栏 Sessions。应用重开后恢复最近会话和草稿。
- **查看子代理**：点击工具栏 Subagents 查看任务、消息、工具输出和结束原因，通过 Refresh 更新提交结果。
- **中文和主题**：点击工具栏 Preferences，选择简体中文、浅色/深色或系统主题并保存。
- **Host 错误**：使用恢复对话框的 Restart 重启，View logs 打开日志目录。反馈问题时附版本、复现步骤和截图，分享前检查日志中是否含自己的项目内容。

## 数据、更新与卸载

会话、设置和凭据默认保存在当前用户的 `~/.dsh` 下；草稿和界面偏好保存在本机应用数据目录。API key 由 DSH 本地管理，并非加密的系统密钥库。不要把这些目录作为反馈附件或复制到公共位置。

此测试版没有自动更新。收到新版本后，退出应用，再运行新的安装器覆盖安装。卸载不会删除 `~/.dsh` 下的会话和凭据。

首发范围为 Windows x64。干净 Windows VM 和本人的付费模型回合仍需试用验证；macOS、自动更新和后台跨会话交互在后续版本迭代。
