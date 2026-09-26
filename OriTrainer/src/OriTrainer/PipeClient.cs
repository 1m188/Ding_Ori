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

    ---- 注入前必须先等托管运行时就绪 ----
    这是本项目踩过的最贵的一个坑，务必看懂再改。

    mono.dll 在进程起来约 40ms 就映射好了，但托管运行时要到约 500ms 才装载完。
    在这中间的窗口里注入，mono_runtime_invoke 会在 mono_thread_attach 里解引用
    一个尚未初始化的指针 —— Mono 以 0xC0000005 抛出、无人接管，游戏当场崩掉。
    实测崩溃栈上的非模块地址（0x065D000C）正是 injector 自己那段 stub，
    偏移 +12 恰好是 "call mono_thread_attach" 的返回地址。

    为什么是"等"而不是"探"：在目标进程里跑代码去判断就绪，本身就是崩溃的来源
    —— 崩掉的那次就是拿到了一个看着像样、实则半成品的 root domain。
    所以判据只能取【游戏自己写的东西】：它每次启动都会清空
    OriDE_Data\output_log.txt，并在运行时装载完时写下 ReadyMarker。

    两个条件缺一不可：
      1. 日志的写入时间晚于进程启动。清空发生在约 50ms 处，在那之前文件里是
         【上一次运行】的内容，同样含就绪标记 —— 只看内容会在 0ms 就误判就绪，
         比不检查还糟。
      2. 内容含就绪标记，即运行时真正装载完毕。

    先开游戏、后开修改器时这两个条件立刻成立，等于没有延迟；先开修改器则最多
    多等约 0.7 秒。这个代价换的是"游戏不会被修改器打崩"。
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

namespace OriTrainer
{
    internal static class PipeClient
    {
        private const string ProcessName = "oriDE";          // 游戏进程名（不带 .exe）
        private const string PayloadName = "OriTrainerDLL"; // 注入载荷名，同时也是内嵌资源名
        private const int TickMs = 200;     // 状态巡检间隔
        private const int ConnectMs = 1000; // 探到空闲实例后的连接超时

        // WaitNamedPipe 要完整路径，NamedPipeClientStream 只要名字，所以前缀只在本地拼。
        private const string PipePrefix = @"\\.\pipe\";

        private const int ErrorFileNotFound = 2; // WaitNamedPipe 报这个才是"管道不存在"

        // 托管运行时就绪标记：Unity 在托管程序集全部装载完之后写进 output_log.txt。
        // 详见文件头"注入前必须先等托管运行时就绪"。
        private const string ReadyMarker = "Completed reload";

        // 日志相对游戏 exe 的位置。exe 在 <游戏目录>\oriDE.exe，
        // 日志在 <游戏目录>\OriDE_Data\output_log.txt。
        private const string LogRelativePath = "OriDE_Data\\output_log.txt";

        private static readonly object _gate = new object();

        // 当前连接；null 表示没有。只有后台线程写。
        private static NamedPipeClientStream _pipe;

        // 已经跑过 Load() 的进程；0 表示还没有。用于防止对同一个进程重复注入。
        private static int _injectedPid;

        // 当前游戏进程的启动时刻（UTC）与日志路径，随进程一起更新。
        // 日志的"这次是新的"就靠跟这个时间比。
        private static DateTime _gameStartUtc;
        private static string _logPath;

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
            Process game = FindGame();
            int pid = game == null ? 0 : game.Id;

            // 换了进程（含游戏退出，此时 pid 为 0）：旧管道一律作废，注入记录也一起清 ——
            // 新进程必然是另一个进程，绝不可能被本程序注入过。
            // 顺带覆盖了 PID 被复用的情况（旧进程死、新进程恰好拿到同一个 PID）。
            if (pid != Status.Pid)
            {
                Drop();
                Status.Pid = pid;
                _injectedPid = 0;

                _gameStartUtc = DateTime.MinValue;
                _logPath = null;
            }

            // 就绪判断的两个依据都取自进程对象，而读取它们可能失败（权限、时机）。
            // 所以不做成"换进程时读一次"，而是【缺了就补】—— 否则一次偶然的读取失败
            // 会让 _logPath 永远为 null，界面就永远停在"未连接"，且再也无法自愈。
            if (game != null && (_logPath == null || _gameStartUtc == DateTime.MinValue))
                ReadTargetInfo(game);

            game?.Dispose();

            if (pid == 0) { Status.Connection = ConnectionState.NoGame; return; }

            // 已经连着就什么都不用做。管道断了会先被上面那句发现（进程不在了），
            // 所以这里不探活 —— 探一个健康管道要处理 WriteFile 阻塞等一堆边角，不值。
            if (_pipe != null) { Status.Connection = ConnectionState.Connected; return; }

            ConnectResult result = TryConnect(pid);

            if (result == ConnectResult.Connected) { Status.Connection = ConnectionState.Connected; return; }
            if (result == ConnectResult.Busy) { Status.Connection = ConnectionState.Waiting; return; }

            // 管道确实不存在。只有"这个进程还没注入过"才动手（原因见文件头）。
            if (_injectedPid == pid) { Status.Connection = ConnectionState.Waiting; return; }

            // 运行时就绪之前绝不注入 —— 否则会把游戏打崩（原因见文件头）。
            // 没就绪就停在"未连接"，下一轮再看，界面也如实显示成没连上。
            if (!RuntimeReady()) { Status.Connection = ConnectionState.Waiting; return; }

            Status.Connection = ConnectionState.Injecting;

            // 注入失败不放弃：注入本身也可能因为别的原因失败，下一轮再试就好。
            if (!Inject(pid)) { Status.Connection = ConnectionState.Waiting; return; }

            _injectedPid = pid;
            Status.Connection = TryConnect(pid) == ConnectResult.Connected // Load() 返回即代表管道已建好
                ? ConnectionState.Connected
                : ConnectionState.Waiting;
        }

        // 目标进程的托管运行时是否已装载完毕。判据见文件头。
        //
        // 只读文件、不碰目标进程：任何"在目标里跑代码探一下"的做法都是崩溃来源。
        // 读不到就一律当成"没就绪"，继续等 —— 保守方向是安全的。
        private static bool RuntimeReady()
        {
            // 两个依据缺一不可。少了启动时刻就没法区分"这次的日志"和"上次的残留"，
            // 那时只能退化成"只看内容"，而那个判据是错的（会在 0ms 就放行）。
            // 宁可停在"未连接"也不退回错误判据：前者只是连不上，后者会把游戏打崩。
            if (_logPath == null || _gameStartUtc == DateTime.MinValue) return false;

            try
            {
                // 条件 1：这份日志得是本次运行写的。
                // 游戏启动约 50ms 时才清空日志，在那之前文件里是上一次运行的内容、
                // 同样含就绪标记 —— 只看内容会在 0ms 就误判就绪，比不检查还糟。
                // （取写入时间不需要打开文件，不受下面说的占用问题影响。）
                if (File.GetLastWriteTimeUtc(_logPath) <= _gameStartUtc) return false;

                // 条件 2：运行时确实装载完了。
                // 读日志内容。
                //
                // ⚠ 必须显式用 FileShare.ReadWrite，不能用 File.ReadAllText。
                //
                // File.ReadAllText 内部是 FileShare.Read，而共享检查是【双向】的：
                // 新打开者声明的 share 必须同时涵盖"对方已有的访问权"。游戏在整个运行期间
                // 都持有一个【写】句柄（日志是持续追加的），于是
                //     FileShare.Read 涵盖 FileAccess.Write ? 否
                // 打开必然抛 IOException，被上面的 catch 吞掉 → 判据永远为假 →
                // 界面永远停在"未连接"，先开哪个都一样。这个坑很隐蔽：
                // 只要游戏没在跑（文件没人占用）就一切正常，一跑起来就必然失败。
                //
                // 另外加 FileShare.Delete，避免别的进程（含游戏自己轮转日志）被我们挡住。
                using (FileStream fs = new FileStream(_logPath, FileMode.Open,
                       FileAccess.Read, FileShare.ReadWrite | FileShare.Delete))
                using (StreamReader r = new StreamReader(fs))
                {
                    return r.ReadToEnd().Contains(ReadyMarker);
                }
            }
            catch
            {
                return false; // 文件还不存在 / 路径不对：都继续等
            }
        }

        // 读取就绪判断所需的两项：进程真正的启动时刻、以及由它推出的日志路径。
        //
        // StartTime 必须是【进程真正的启动时刻】，不能拿"本程序第一次看到它"顶替：
        // 正常用法是游戏早就绪、之后才开修改器，那样日志的写入时间必然早于
        // "第一次看到"，判据会永远不成立、永远连不上。
        //
        // 两项都读失败时保持"没就绪"，即不注入。这是安全的僵局而非缺陷：
        // 读 StartTime 只要 PROCESS_QUERY_INFORMATION，注入要 PROCESS_ALL_ACCESS，
        // 前者是后者的子集 —— 连启动时刻都读不到时，注入本来也必然失败。
        private static void ReadTargetInfo(Process game)
        {
            if (_gameStartUtc == DateTime.MinValue)
            {
                try { _gameStartUtc = game.StartTime.ToUniversalTime(); } catch { }
            }

            if (_logPath == null)
            {
                try
                {
                    string exe = game.MainModule.FileName;
                    if (!string.IsNullOrEmpty(exe))
                        _logPath = Path.Combine(Path.GetDirectoryName(exe), LogRelativePath);
                }
                catch { }
            }
        }

        private static Process FindGame()
        {
            Process[] games = Process.GetProcessesByName(ProcessName);

            for (int i = 1; i < games.Length; i++) games[i].Dispose(); // 只留第一个，其余立刻释放

            return games.Length > 0 ? games[0] : null;
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
                    // 载荷由 csproj 内嵌进本 exe，资源名见 OriTrainer.csproj 的 LogicalName
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
            using (MemoryStream ms = new MemoryStream())
            {
                // 必须用 CopyTo 而不是单次 Read：Stream.Read 按契约只保证"至少读一个字节"，
                // 不保证填满缓冲区。短读会让载荷被截断，症状是注入莫名其妙失败、万一出问题很难排查。
                s.CopyTo(ms);
                return ms.ToArray();
            }
        }

        [DllImport("kernel32.dll", CharSet = CharSet.Unicode, SetLastError = true)]
        private static extern bool WaitNamedPipe(string name, int timeout);
    }
}
