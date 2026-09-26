using System;
using System.Reflection;
using System.Threading;

namespace OriTrainerDLL.Features
{
    // 死亡数归零：持续把 SeinDeathCounter.m_deathCounter 保持为 0。
    // 实现同终极版：不走 public 的 Count setter（它带存档副作用），
    // 反射直写私有字段 m_deathCounter；停止不还原（该字段是存档字段，存过档 0 已落盘）。
    public static class ZeroDeaths
    {
        private const int IntervalMs = 10;

        private static readonly BindingFlags Private =
            BindingFlags.NonPublic | BindingFlags.Instance;

        private static FieldInfo _fCount; // m_deathCounter
        private static Timer _timer;

        public static void Start()
        {
            if (_timer != null) return; // 幂等：重复 Start 不重复起定时器

            // 字段名对不上就直接失败（Loader 会记进错误日志），而不是每 10ms 静默空转
            _fCount = typeof(SeinDeathCounter).GetField("m_deathCounter", Private) ?? throw new Exception("SeinDeathCounter 的字段名与预期不符，功能无法工作");

            _timer = new Timer(Tick, null, 0, IntervalMs);
        }

        public static void Stop()
        {
            if (_timer == null) return;

            _timer.Dispose();
            _timer = null;
        }

        private static void Tick(object state)
        {
            // 定时器回调里的未捕获异常会终止整个进程（即游戏），必须自己兜住
            try
            {
                SeinDeathCounter counter = SeinDeathCounter.Instance;
                if (counter == null) return;

                // 走 public 的 Count 读（无副作用），走反射写（绕开 setter 的存档动作）。
                // 正常游玩时写入次数 ≈ 死亡次数，而不是每秒 100 次。
                if (SeinDeathCounter.Count > 0)
                    _fCount.SetValue(counter, 0);
            }
            catch { }
        }
    }
}
