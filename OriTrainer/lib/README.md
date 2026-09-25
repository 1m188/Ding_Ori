# lib —— 游戏程序集副本

这里存放从游戏安装目录复制过来的托管程序集，供 `OriTrainerDEDLL` **编译期引用**。

## 由来

| 项 | 值 |
|---|---|
| 游戏 | Ori and the Blind Forest: Definitive Edition（奥日与迷失森林：终极版） |
| Steam App ID | `387290` |
| Unity 版本 | `5.3.2f1`（读自 `oriDE.exe` 的 PE 版本资源） |
| 来源目录 | `<游戏安装目录>\oriDE_Data\Managed\` |

文件校验值（SHA256，可用来确认与游戏本体是否一致）：

| 文件 | SHA256 |
|---|---|
| `Assembly-CSharp.dll` | `6F3A0384059D38DA3183AD7D849C6BD352EA5555F3C4D019D6CA46453D9E8EAA` |
| `UnityEngine.dll` | `F98945F391961D1D86D7E891834137E584DB7678270F68365195A9D48AEC4B47` |
| `Assembly-CSharp-firstpass.dll` | `E817AD34C43B16695D7877C78CD01C54EBAB1735B632BA5B886648EF1DF06DB5` |


## 各自的作用

### `Assembly-CSharp.dll`

游戏自己的全部逻辑代码，是修改器的主要操作对象。里面包含：

- `Game.Characters` —— 持有 `Sein`、`BabySein`、`Naru` 等静态字段
- `SeinCharacter` —— 玩家角色的实例类型
- `HeroController`、`PlayerAbilities`、`SaveGameController` 等

`Game.Characters.Sein` 是 `public static` 字段，注入后直接读写即可，
不需要像旧版 Go 修改器那样扫描特征码定位地址。

### `UnityEngine.dll`

Unity 引擎的运行库。修改器里凡是碰到 `GameObject`、`Transform`、`Vector3`、
`Time` 之类的类型，都由它提供。多数情况下并不直接"用"它做功能，
但只要代码里访问 `sein.gameObject`，编译期就必须能解析这个引用。

### `Assembly-CSharp-firstpass.dll`

游戏的 firstpass 程序集，存放 Unity 的 Plugins 目录下会被更早编译的代码。
目前功能实现尚未用到，**为减少后续返工先一并复制**。若确定用不到可以删除，
同时移除 csproj 里对应的 `<Reference>` 项。

## 为什么是复制而不是引用原目录

- 游戏已停止更新，程序集版本固定，复制一份不会失效。
- 避免在项目文件里写死各人不同的本机游戏路径（那既是环境耦合，也会泄露本地目录结构）。
- 克隆仓库后无需安装游戏即可编译。

**代价**：游戏若日后更新，需要重新复制并更新上表中的校验值。

## 引用方式

`src\OriTrainerDEDLL\OriTrainerDEDLL.csproj` 中通过 `HintPath` 引用：

```xml
<Reference Include="UnityEngine">
  <HintPath>..\..\lib\UnityEngine.dll</HintPath>
  <Private>false</Private>
</Reference>
```

### `Private=false` 不能去掉

它表示「编译期解析类型用，但不要复制到输出目录」。

若设为 `true`（默认值），这些程序集会被复制进 `bin\`，后果有两个：

1. 发布包平白多出无用的副本；
2. 更严重的是，若这些副本被放到游戏目录附近，游戏可能加载到副本而非原版，
   造成难以排查的问题。

这些程序集在游戏进程里本来就已经加载，**运行时由游戏自己提供**，
绝不能随注入载荷分发。

## 重要：只在 DLL 项目里引用，exe 项目不要引用

- `OriTrainerDEDLL`（net35，注入载荷）→ **需要**这些引用，它要直接操作游戏类型。
- `OriTrainerDE`（net48，控制台修改器）→ **不需要**。它运行在游戏进程之外，
  只负责注入和通过命名管道发命令，不接触任何游戏类型。

## 关于版权

这些文件是游戏的组成部分，版权归 Moon Studios / Microsoft 所有，
此处仅用于本机开发时提供编译期类型信息。

## 载荷不会因此变大

引用游戏程序集**不会**把游戏代码编进 `OriTrainerDEDLL.dll`。
载荷比游戏程序集本身小两个数量级。

原因是编译产物里只记录「类型名 + 成员签名」的引用条目（TypeRef / MemberRef），
不含任何实现体。运行时由游戏进程内的 Mono 按名字解析到已经加载的游戏程序集。
