/*
    管道连接管理：后台线程持续把"与游戏内载荷的连接"收敛到正确状态。

    ---- 为什么是后台线程 ----
    连接不再是"启动时做一次"的事：游戏会退出、会崩溃、会重启，管道随时可能断。
    与其在启动阶段写一套"找进程 → 注入 → 连接"、再在主循环里写另一套"发现断了 →
    重连"，不如只留一套 —— 后台线程每 200ms 重新判断一次自己该处于什么状态，
    于是启动时的第一次判断和运行中的任何一次判断走的是完全相同的代码。

    ---- 对外只有三件事 ----
        Start()            起后台线程，立即返回
        Stop()             断开管道、停线程（DLL 借这次断开停掉全部功能）
        TrySend(command)   发一条命令，只读，失败返回 false

    ---- 谁写什么 ----
        Status.Pid / Status.Connection   只有本文件的后台线程写
        Feature.On                       只有主循环写
        _pipe / _injectedPid             只有本文件写（TrySend 只读 _pipe）
    没有任何字段有两个写入者，所以除了 _pipe（后台线程要 Dispose、主循环要 Write，
    两者会撞上，用 _gate 保护）之外不需要任何同步。

    ---- 只有"管道不存在"才允许注入 ----
    注入是单向的，而且 Load() 每次都会新建一个管道实例 + 起一个 Serve 线程。
    对同一个进程注入两次 = 游戏里有两个 Serve 各自调 Shutdown()、命令随机落到
    不同实例上，表现是"按了没反应"和"开着开着突然全关了"，且完全查不出原因。
    所以：先连、连不上才注入；管道存在（哪怕连不上）就绝不注入。
    这条顺序还顺带保证了一件事 —— 万一某次注入其实成功了、只是返回前出了岔子，
    下一轮会先看到管道、直接连上，而不是再注一次。

    ---- 唯一的漏洞及其堵法 ----
    TrySend 失败时只返回 false、不做任何补救，这依赖一条前提：
    "游戏进程活着 ⟹ 管道一定存在，且一定有人在读"。后者由 Loader.Serve 的死循环
    保证 —— 那个线程一旦退出，管道句柄还在、进程也还在，进程扫描发现不了，
    于是永远连不上也永远不重连。Loader.Serve 的 try/catch 就是为此存在。
*/

using System;
using System.Diagnostics;
using System.IO;
using System.IO.Pipes;
using System.Reflection;
using System.Runtime.InteropServices;
using System.Text;
using System.Threading;
using OriTrainerShared;
using SharpMonoInjector;

namespace OriTrainerDE
{
    internal static class PipeClient
    {
        private const string ProcessName = "oriDE";          // 游戏进程名（不带 .exe）
        private const string PayloadName = "OriTrainerDEDLL"; // 注入载荷名，同时也是内嵌资源名
        private const int TickMs = 200;     // 状态巡检间隔
        private const int ConnectMs = 1000; // 探到空闲实例后的连接超时

        // WaitNamedPipe 要完整路径，NamedPipeClientStream 只要名字，所以前缀只在本地拼。
        private const string PipePrefix = @"\\.\pipe\";

        private const int ErrorFileNotFound = 2; // WaitNamedPipe 报这个才是"管道不存在"

        private static readonly object _gate = new object();

        // 当前连接；null 表示没有。只有后台线程写。
        private static NamedPipeClientStream _pipe;

        // 已经跑过 Load() 的进程；0 表示还没有。用于防止对同一个进程重复注入。
        private static int _injectedPid;

        private static volatile bool _running;

        public static void Start()
        {
            if (_running) return; // 幂等

            _running = true;
            new Thread(Watch) { IsBackground = true }.Start();
        }

        // 退出时调用。这次断开就是 DLL 侧的"修改器已关闭"信号（见 Loader.Serve），
        // 全部功能在那里被停掉。
        public static void Stop()
        {
            _running = false;
            Drop();
        }

        // 发一条命令。只读：不改连接状态、不改功能开关。
        //
        // 失败不需要在这里补救：发送失败只可能是管道断了，而管道断了只可能是游戏进程
        // 不在了（管道建好之后 DLL 侧就再没关过它），进程扫描每一轮都在做，
        // 下一轮必然发现并重连。这里唯一的责任就是把这条发出去。
        public static bool TrySend(string command)
        {
            byte[] bytes = Encoding.UTF8.GetBytes(command + Constants.Terminator);

            lock (_gate)
            {
                if (_pipe == null) return false;

                try
                {
                    // 不调 Flush：管道是字节流，WriteFile 直接把数据发出去，无需刷新；
                    // 而 FlushFileBuffers 对管道的语义是"阻塞到对端把数据读完"，
                    // 万一 DLL 卡住会把主循环一起拖死。
                    _pipe.Write(bytes, 0, bytes.Length);
                    return true;
                }
                catch
                {
                    return false;
                }
            }
        }

        private static void Watch()
        {
            while (_running)
            {
                // 后台线程上的未捕获异常会终止整个进程（即修改器）。巡检循环绝不能死。
                try { Tick(); }
                catch { }

                Thread.Sleep(TickMs);
            }
        }

        // 一次状态巡检：把"此刻应该处于什么连接状态"收敛到位。
        private static void Tick()
        {
            int pid = FindGame();

            // 换了进程（含游戏退出，此时 pid 为 0）：旧管道一律作废，注入记录也一起清 ——
            // 新进程必然是另一个进程，绝不可能被本程序注入过。
            // 顺带覆盖了 PID 被复用的情况（旧进程死、新进程恰好拿到同一个 PID）。
            if (pid != Status.Pid)
            {
                Drop();
                Status.Pid = pid;
                _injectedPid = 0;
            }

            if (pid == 0) { Status.Connection = ConnectionState.NoGame; return; }

            // 已经连着就什么都不用做。管道断了会先被上面那句发现（进程不在了），
            // 所以这里不探活 —— 探一个健康管道要处理 WriteFile 阻塞等一堆边角，不值。
            if (_pipe != null) { Status.Connection = ConnectionState.Connected; return; }

            ConnectResult result = TryConnect(pid);

            if (result == ConnectResult.Connected) { Status.Connection = ConnectionState.Connected; return; }
            if (result == ConnectResult.Busy) { Status.Connection = ConnectionState.Waiting; return; }

            // 管道确实不存在。只有"这个进程还没注入过"才动手（原因见文件头）。
            if (_injectedPid == pid) { Status.Connection = ConnectionState.Waiting; return; }

            Status.Connection = ConnectionState.Injecting;

            // 注入失败不放弃：修改器和游戏同时启动时 mono.dll 可能还没加载完，
            // 注入必然失败，下一轮再试就好。
            if (!Inject(pid)) { Status.Connection = ConnectionState.Waiting; return; }

            _injectedPid = pid;
            Status.Connection = TryConnect(pid) == ConnectResult.Connected // Load() 返回即代表管道已建好
                ? ConnectionState.Connected
                : ConnectionState.Waiting;
        }

        private static int FindGame()
        {
            Process[] games = Process.GetProcessesByName(ProcessName);

            try
            {
                return games.Length > 0 ? games[0].Id : 0;
            }
            finally
            {
                // 每个 Process 都持有句柄，5Hz 巡检下不释放就是稳定泄漏。
                foreach (Process p in games) p.Dispose();
            }
        }

        // 一次连接尝试的结果。只有 Missing 允许触发注入，
        // 其余任何失败都算 Busy：宁可等下一轮，也绝不重复注入。
        private enum ConnectResult
        {
            Connected, // 连上了
            Busy,      // 没连上，且不能注入（管道被占着 / 本程序正在退出）
            Missing,   // 管道不存在，可以注入
        }

        private static ConnectResult TryConnect(int pid)
        {
            // WaitNamedPipe 是唯一能区分"管道不存在"和"管道被占用"的手段：
            //   不存在       → 载荷不在，可以注入
            //   在但被占着   → 已经有客户端连着（另一个修改器实例，或我们自己的旧连接）
            // 超时传 1 而不是 0：0 是 NMPWAIT_USE_DEFAULT_WAIT 这个特殊常量，不是"不等"。
            if (!WaitNamedPipe(PipePrefix + Constants.PipeName(pid), 1))
                return Marshal.GetLastWin32Error() == ErrorFileNotFound
                    ? ConnectResult.Missing
                    : ConnectResult.Busy;

            // 探到实例空闲，Connect 是瞬时的；给 1 秒只是防"探到与连上之间被抢走"。
            NamedPipeClientStream pipe =
                new NamedPipeClientStream(".", Constants.PipeName(pid), PipeDirection.Out);

            try
            {
                pipe.Connect(ConnectMs);
            }
            catch
            {
                pipe.Dispose(); // 连接失败也持有句柄，不释放会泄漏
                return ConnectResult.Busy;
            }

            lock (_gate)
            {
                // Stop() 之后不再挂新句柄，否则退出瞬间会留下一个没人释放的连接。
                if (!_running) { pipe.Dispose(); return ConnectResult.Busy; }

                _pipe = pipe;
            }

            return ConnectResult.Connected;
        }

        // Dispose 会关掉客户端句柄，DLL 侧的 ReadFile 随即失败、走完
        // DisconnectNamedPipe + Shutdown —— 也就是说功能会在那边被停掉。
        private static void Drop()
        {
            lock (_gate)
            {
                if (_pipe == null) return;

                _pipe.Dispose();
                _pipe = null;
            }
        }

        private static bool Inject(int pid)
        {
            try
            {
                using (Injector injector = new Injector(pid))
                {
                    // 载荷由 csproj 内嵌进本 exe，资源名见 OriTrainerDE.csproj 的 LogicalName
                    injector.Inject(ReadPayload(PayloadName + ".dll"), PayloadName, "Loader", "Load");
                }
                return true;
            }
            catch
            {
                return false;
            }
        }

        private static byte[] ReadPayload(string logicalName)
        {
            // 获取要注入的载荷的数据
            using (Stream s = Assembly.GetExecutingAssembly().GetManifestResourceStream(logicalName))
            {
                byte[] payload = new byte[s.Length];
                s.Read(payload, 0, payload.Length);
                return payload;
            }
        }

        [DllImport("kernel32.dll", CharSet = CharSet.Unicode, SetLastError = true)]
        private static extern bool WaitNamedPipe(string name, int timeout);
    }
}
