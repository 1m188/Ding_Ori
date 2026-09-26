// 假游戏：测试专用的对端进程，模拟真实游戏里与本项目相关的那部分行为。
//
// 它做两件事，各自对应被测代码的一条路径：
//   1. 像 DLL 的 Loader 那样建同名管道、连上、逐字节读到 '\n'、把每行命令记进日志
//      —— 这是"连接管理"和"命令流"两套断言的唯一依据。
//   2. （可选）像 Unity 那样写 <数据目录>\output_log.txt：先清空、后写就绪标记，
//      并可长期持有写句柄 —— 这是"就绪门"那套断言的依据（见 PipeClient 文件头）。
//
// ---- 命令行 ----
//   <进程名>.exe <命令日志> [建管道延迟ms]
//       仅管道模式。延迟用来制造"进程已存在、管道还没出现"的窗口，
//       逼 PipeClient 走进注入分支（本程序没有 mono.dll，注入必然失败）。
//
//   <进程名>.exe <命令日志> <游戏日志路径> <清空时刻ms> <就绪时刻ms> [noPipe] [hold]
//       带日志模式。noPipe=不建管道（保证走注入分支）；hold=长期持有日志写句柄
//       （复现 Unity 的行为，也是 File.ReadAllText 会失败的那个场景）。
//
// 两种模式靠"第二个参数是不是整数"区分：路径永远解析不成整数。
//
// ---- 为什么产物名要按 Edition 区分 ----
// PipeClient 是按进程名扫描的（终极版 "oriDE"、原版 "ori"），且由【进程主模块
// 所在目录】推算日志路径。所以测试项目会把它放到两个地方：
//   <测试输出>\<进程名>.exe           管道模式用（日志路径无关紧要）
//   <测试输出>\FakeGame\<进程名>.exe  日志模式用（日志必须是同级 <数据目录>\output_log.txt）
// 产物名由 csproj 按 Edition 决定（oriDE.exe / ori.exe），本文件无需关心具体名字。

using System;
using System.Diagnostics;
using System.IO;
using System.Runtime.InteropServices;
using System.Text;
using System.Threading;

static class FakeGame
{
    const uint PIPE_ACCESS_DUPLEX = 0x3;
    const uint PIPE_TYPE_BYTE = 0x0;
    const uint PIPE_READMODE_BYTE = 0x0;
    const uint PIPE_WAIT = 0x0;
    const uint PIPE_UNLIMITED_INSTANCES = 255;

    [DllImport("kernel32.dll", CharSet = CharSet.Unicode, SetLastError = true)]
    static extern IntPtr CreateNamedPipeW(string name, uint openMode, uint pipeMode,
        uint maxInstances, uint outBuf, uint inBuf, uint timeout, IntPtr sec);

    [DllImport("kernel32.dll", SetLastError = true)]
    static extern bool ConnectNamedPipe(IntPtr pipe, IntPtr ov);

    [DllImport("kernel32.dll", SetLastError = true)]
    static extern bool DisconnectNamedPipe(IntPtr pipe);

    [DllImport("kernel32.dll", SetLastError = true)]
    static extern bool ReadFile(IntPtr h, [Out] byte[] buf, uint toRead, out uint read, IntPtr ov);

    static string _cmdLog;
    static FileStream _hold; // 长期持有的日志写句柄

    static void Main(string[] args)
    {
        if (args.Length < 1)
        {
            Console.Error.WriteLine("用法见本文件头注释");
            return;
        }

        _cmdLog = args[0];
        Log("PROC_START pid=" + Process.GetCurrentProcess().Id);

        // 第二个参数是整数 -> 仅管道模式（该整数即建管道前的延迟）
        bool pipeOnly = args.Length < 2 || int.TryParse(args[1], out _);
        if (pipeOnly)
        {
            int pipeDelay = 0;
            if (args.Length >= 2) int.TryParse(args[1], out pipeDelay);
            if (pipeDelay > 0) Thread.Sleep(pipeDelay);
            RunPipeServer();
            return;
        }

        // 否则 -> 带日志模式
        string gameLog = args[1];
        int truncateMs = args.Length > 2 ? int.Parse(args[2]) : 50;
        int readyMs = args.Length > 3 ? int.Parse(args[3]) : 600;
        bool noPipe = args.Length > 4 && args[4] == "noPipe";
        bool hold = args.Length > 5 && args[5] == "hold";

        // 阶段 0：这段时间内文件里是【上一次运行】的残留（由调用方预先写好）
        if (truncateMs > 0) Thread.Sleep(truncateMs);

        // 阶段 1：清空。真实游戏启动约 50ms 时这么做，就绪门"条件 1"防的就是这段残留。
        try { File.WriteAllText(gameLog, ""); } catch { }
        Log("TRUNCATED");

        // 阶段 2：写就绪标记（等于 Unity 的 "Completed reload"）
        Thread.Sleep(Math.Max(0, readyMs - truncateMs));
        try
        {
            File.AppendAllText(gameLog, "Initialize engine version: 5.3.2f1\n");
            File.AppendAllText(gameLog, "Begin MonoManager ReloadAssembly\n");
            File.AppendAllText(gameLog, "Completed reload, in 0.593 seconds\n");
        }
        catch { }
        Log("READY_MARKER_WRITTEN");

        // hold：用【写】访问权长期持有日志。Unity 就是这样（日志持续追加），
        // 也正是让 File.ReadAllText（内部 FileShare.Read）必然抛 IOException 的场景。
        if (hold)
        {
            _hold = new FileStream(gameLog, FileMode.Append, FileAccess.Write, FileShare.Read);
            Log("HOLDING");
        }

        if (noPipe)
        {
            Log("NO_PIPE_MODE");
            Thread.Sleep(Timeout.Infinite);
            return;
        }

        RunPipeServer();
    }

    static void RunPipeServer()
    {
        string name = @"\\.\pipe\oritrainer_" + Process.GetCurrentProcess().Id;
        IntPtr pipe = CreateNamedPipeW(name, PIPE_ACCESS_DUPLEX,
            PIPE_TYPE_BYTE | PIPE_READMODE_BYTE | PIPE_WAIT,
            PIPE_UNLIMITED_INSTANCES, 4096, 4096, 0, IntPtr.Zero);

        if (pipe == new IntPtr(-1)) { Log("CREATE_FAILED"); return; }

        Log("PIPE_READY " + name);

        while (true)
        {
            ConnectNamedPipe(pipe, IntPtr.Zero);
            Log("CONNECTED");
            Read(pipe);
            DisconnectNamedPipe(pipe);
            Log("DISCONNECTED");
        }
    }

    // 与真实 Loader.ReadCommands 同样的逐字节读法：只能靠 ReadFile 失败来判断对端断开。
    static void Read(IntPtr pipe)
    {
        byte[] one = new byte[1];
        StringBuilder line = new StringBuilder();

        while (ReadFile(pipe, one, 1, out uint read, IntPtr.Zero) && read == 1)
        {
            if (one[0] == '\n') { Log("CMD " + line); line.Length = 0; }
            else line.Append((char)one[0]);
        }
    }

    static void Log(string s)
    {
        try { File.AppendAllText(_cmdLog, DateTime.Now.ToString("HH:mm:ss.fff") + " " + s + Environment.NewLine); }
        catch { }
    }
}
