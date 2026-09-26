# OriTrainer

奥日与迷失森林修改器，C#版，采用monoAPI注入的方式

## 项目逻辑
每个修改器分为两个部分：修改器exe和要注入的dll，修改器启动后向游戏注入dll，dll操作游戏内容，如写入某些字段等，修改器和dll通过IPC管道通信，玩家使用修改器启动/关闭对应功能的开关，修改器发送这些指令到游戏进程的dll中，然后dll再操作游戏，从而实现对游戏功能的注入

## 项目架构
- 修改器exe侧采取终端界面，通过在一个主循环中不断的检查管道连接、检查按键并设置状态、根据状态绘制UI界面等操作来为用户提供一个不间断的修改器界面呈现
- 注入游戏的DLL通过从管道中不断接收命令，并且将命令通过反射调用对应静态方法来实现功能的启动和停止

## 通信协议
修改器exe通过管道向被注入进游戏的dll发送消息，如 UnlimitedLife Start 或者 UnlimitedLife Stop 来控制功能的启动和停止，没有返回，dll侧只是收到消息并且执行功能，不存储功能开关状态（开关由exe侧记录，dll侧的 Start/Stop 都是幂等的）

## 连接与断开
exe侧全程维护一条长连接：游戏退出、崩溃、重启后会自动重连，注入也由后台线程按状态自动完成，界面上的连接状态实时反映当前处于哪一步。

需要注意 DLL 侧的行为：**每次"连上"和"断开"都会停掉全部功能**（`Loader.Serve` 在两个时机各调一次 `Shutdown`）。这意味着
- 关闭修改器 / 游戏退出时，所有功能会被自动停掉，不需要逐个手动关；
- 但它也决定了**"一条命令连一次管道"的用法不成立** —— 那样每条命令刚执行完就会被断开时的 Shutdown 撤销掉。
- 注入是单向的，一个游戏进程只注入一次，之后靠重连而不是重新注入（原因见 `vendor/SharpMonoInjector/README.md`）。

## 目录结构
- [src](./src/) 源码目录
    - [OriTrainer](./src/OriTrainer/) 奥日与迷失森林终极版修改器
    - [OriTrainerDLL](./src/OriTrainerDLL/) 奥日与迷失森林终极版修改器将要向游戏中注入的DLL
- [lib](./lib/) 游戏程序集，用于给要注入的dll引用API，从而在注入后能够操作游戏内容
- [vendor](./vendor/) sharpmonoinjector项目中注入dll相关的API，修改器exe通过这些API将dll注入到游戏中去
- [tests](./tests/) 测试套件（假游戏 + 测试项目），跑法见 [tests/README](./tests/README.md)

## 注意
- 所有项目的构建平台目标都必须是x86，因为游戏是x86的
- 修改器需要管理员权限（注入要 `PROCESS_ALL_ACCESS`），已通过 `app.manifest` 声明
- `OriTrainer` / `OriTrainerDLL` 两个项目的 x86 是靠 `RuntimeIdentifier=win-x86` 得到的（它们没设 `PlatformTarget`），不要删掉这一项
