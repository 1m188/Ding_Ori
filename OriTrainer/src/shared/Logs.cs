/*
    整个修改器统一写日志的工具：所有日志都写进同一个 %TEMP%\OriTrainer.log。

    命名空间走 OriTrainerShared：本文件同时编进 net48 的 exe 与 net35 的 dll
    （见两个 csproj 的 <Compile Include="..\shared\**\*.cs">）。

    调用方只负责给内容（日志内容本身），路径、文件名、时间戳、线程安全
    都由本工具内部管理，调用方无需关注，也不传日志名。

    线程安全：使用 lock 同步、调用方阻塞等待、同步追加写。不用异步/后台队列，
    因为 DLL 跑在 net35 + 旧 Mono 2.x 注入上下文里，旧 Mono 的后台线程调度本身
    就不可靠（System.Threading.Timer 回调曾被证实不触发），异步写线程是雷区。
    File.AppendAllText 每次自己开文件、写完即关：自愈式（文件被删会重建）、无句柄泄漏。

    Release 剥离：用 [Conditional("DEBUG")] 修饰。Release 下调用点连 IL 都被编译器
    抹掉（调用方无需改代码，只省一次空调用），即"空函数方便编译器优化"。
    注意：方法必须是 void 才能用 [Conditional]。
*/
using System;
using System.Diagnostics;
using System.IO;

namespace OriTrainerShared
{
    public static class Logs
    {
        // 统一的一个日志文件名，所有日志都进这个文件。
        private const string FileName = "OriTrainer.log";

        // 统一的日志写入路径
        private static readonly string LogPath = Path.Combine(Path.GetTempPath(), FileName);

        // 多路调用方（命令线程、hook 操作、主线程/后台线程）并发写时互斥。
        // 同步阻塞式：调用方写好即回，日志工具内部排队逐个追加。
        private static readonly object _sync = new object();

        // Release 构建下此方法为空（[Conditional] 连调用都抹掉），纯 Debug 诊断用。
        [Conditional("DEBUG")]
        public static void Log(string msg)
        {
            try
            {
                lock (_sync)
                {
                    File.AppendAllText(LogPath,
                    string.Format("[{0:yyyy-MM-dd HH:mm:ss.fff}] {1}{2}",
                        DateTime.Now, msg, Environment.NewLine));
                }
            }
            catch { } // 日志写失败不影响主流程
        }
    }
}