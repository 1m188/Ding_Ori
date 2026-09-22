/*
    一些共享常量清单
*/

namespace OriTrainerShared
{
    // 修改器与注入DLL两端共享源码（不是共享程序集）：本文件同时编进 net48 的 exe 与 net35 的 dll。
    // 只放「两端必须逐字节一致、且无法各自推导」的东西。
    public static class Constants
    {
        // 命名管道是字节流，没有消息边界：连发两条命令可能被一次读完，必须有分隔符。
        public const char Terminator = '\n';

        // exe 用游戏进程的 pid，dll 用自身 pid（就是游戏进程），两端必然算出同一个名字。
        public static string PipeName(int pid)
        {
            return "oritrainer_" + pid;
        }
    }
}
