# Savant Home Assistant 代理

这是一个简单的 TCP 代理程序，允许您的 Savant 系统与 Home Assistant 进行通信。

## 安装指南

### 先决条件
1. 已安装并运行 **Home Assistant**。
2. 已设置 **Savant** 系统。
3. 对 Home Assistant Add-on 和 Savant Profile 有基本的了解。

### 第一步：将 Add-on 仓库添加到 Home Assistant
1. 打开 Home Assistant。
2. 前往 **配置** > **加载项、备份与 Supervisor** > **加载项商店** (Supervisor > Add-on Store)。
3. 点击右上角的 **三点菜单**，选择 **仓库** (Repositories)。
4. 粘贴本仓库的 URL。
5. 点击 **添加**，然后从列表中找到并安装 "Savant Home Assistant Proxy" 加载项。

### 第二步：配置 Add-on
1. 安装加载项后，点击 **启动** (Start) 运行它。
2. 按照加载项设置中提供的任何配置说明进行操作（例如配置白名单等）。

### 第三步：下载并导入 Savant Profile
1. 从本仓库下载 `hass_savant.xml` 文件。
2. 将此 Profile 导入到您的 Savant 系统蓝图 (Blueprint) 中：
   - 打开您的 Savant 系统的 **Blueprint Manager**。
   - 将 `Hass Savant` profile 添加到您的配置中。

### 第四步：配置以太网连接
1. 设置 Savant 系统与您网络的 **以太网连接**。
2. 在 **Savant Profile 设置** 中，指定 Home Assistant 实例的 IP 地址，以便两个系统可以通信。
   - 您可以在 Home Assistant 的 **设置** > **系统** > **网络** 部分找到 IP 地址。
   - 如果是在局域网内连接，您可能可以使用 `homeassistant.local` 代替 IP 地址。
   - **端口号** 默认为 `8080`。

### 第五步：添加设备和实体 ID (Entity ID)
1. 在 Savant 系统中，前往您希望集成 Home Assistant 设备的数据表。
2. 添加相应的设备并将其链接到 Home Assistant 实体。

#### 在 Home Assistant 中查找实体 ID：
- 前往 Home Assistant 的 **配置** > **设备与服务** > **实体**。
- 使用搜索功能找到您想要链接到 Savant 系统的特定设备实体。
- 复制设备的 **实体 ID**（例如 `light.living_room_lamp`），并将其添加到 Savant 系统中的相应位置。

### 第六步：验证集成
一旦设置好以太网连接并添加了实体 ID，请测试系统以确保 Savant 系统能够正确地与 Home Assistant 通信。

---

如需更多详细信息和故障排除，请参考官方文档或在本仓库中提交 Issue。
