# OriTrainer

奥日与迷失森林修改器，C#版，采用monoAPI注入的方式

## 项目逻辑
每个修改器分为两个部分：修改器exe和要注入的dll，修改器启动后向游戏注入dll，dll操作游戏内容，如写入某些字段等，修改器和dll通过IPC管道通信，玩家使用修改器启动/关闭对应功能的开关，修改器发送这些指令到游戏进程的dll中，然后dll再操作游戏，从而实现对游戏功能的注入

## 目录结构
- [src](./src/) 源码目录
    - [OriTrainerDE](./src/OriTrainerDE/) 奥日与迷失森林终极版修改器
    - [OriTrainerDEDLL](./src/OriTrainerDEDLL/) 奥日与迷失森林终极版修改器将要向游戏中注入的DLL
- [lib](./lib/) 游戏程序集，用于给要注入的dll引用API，从而在注入后能够操作游戏内容
- [vendor](./vendor/) sharpmonoinjector项目中注入dll相关的API，修改器exe通过这些API将dll注入到游戏中去

## 注意
- 所有项目的构建平台目标都必须是x86，因为游戏是x86的
