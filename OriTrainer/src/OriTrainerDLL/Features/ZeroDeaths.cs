using System;
using System.Reflection;

namespace OriTrainerDLL.Features
{
    // 死亡数归零：持续把 SeinDeathCounter.m_deathCounter 保持为 0。
    // 实现同终极版：不走 public 的 Count setter（它带存档副作用），
    // 反射直写私有字段 m_deathCounter；停止不还原（该字段是存档字段，存过档 0 已落盘）。
    //
    // 挂游戏自己的每帧回调 OnGameFixedUpdate（原版旧 Mono 的 System.Threading.Timer
    // 不可靠，回调不触发，与 UnlimitedEnergy 同理）。
    public static class ZeroDeaths
    {
        private static readonly BindingFlags Private =
            BindingFlags.NonPublic | BindingFlags.Instance;

        private static FieldInfo _fCount; // m_deathCounter
        private static Action _hook;      // 保留引用以便 Stop 时注销

        public static void Start()
        {
            if (_hook != null) return; // 幂等：重复 Start 不重复挂载

            // 字段名对不上就直接失败（Loader 会记进错误日志），而不是每帧静默空转
            _fCount = typeof(SeinDeathCounter).GetField("m_deathCounter", Private) ?? throw new Exception("SeinDeathCounter 的字段名与预期不符，功能无法工作");

            // Scheduler 由 GameController 持有，而 GameController.Awake 是单例守卫
            // （Instance 已存在则 Destroy 自身），所以该回调在整个进程内稳定可用。
            GameScheduler scheduler = Game.Events.Scheduler;
            if (scheduler == null)
                throw new Exception("GameScheduler 尚未就绪（游戏未启动完成），功能无法挂载");

            _hook = OnGameFixedUpdate;
            scheduler.OnGameFixedUpdate.Add(_hook);
        }

        public static void Stop()
        {
            if (_hook == null) return;

            Game.Events.Scheduler.OnGameFixedUpdate.Remove(_hook);
            _hook = null;
        }

        // 由游戏主线程每个 FixedUpdate 调用
        private static void OnGameFixedUpdate()
        {
            // 游戏回调里抛出的异常会顺着 GameController.FixedUpdate 冒到 Unity，
            // 后果不可预期，必须自己兜住
            try
            {
                SeinDeathCounter counter = SeinDeathCounter.Instance;
                if (counter == null) return;

                // 走 public 的 Count 读（无副作用），走反射写（绕开 setter 的存档动作）。
                // 正常游玩时写入次数 ≈ 死亡次数，而不是每秒 50 次。
                if (SeinDeathCounter.Count > 0)
                    _fCount.SetValue(counter, 0);
            }
            catch { }
        }
    }
}