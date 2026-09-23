using System.Threading;

namespace OriTrainerDEDLL.Features
{
    // 灵魂链接无需冷却：每 10ms 调用游戏自己的 FillSoulFlameBar() 把冷却清零。
    //
    // 用游戏方法而不是反射写私有字段 m_cooldownRemaining：
    //   - FillSoulFlameBar() 是 public，游戏自己在"捡到能量容器 / 回血"时也调它；
    //   - 它只写 m_cooldownRemaining = 0 和 m_nagTimer = 0，后者仅抑制"链接已就绪"提示，无副作用；
    //   - 不必反射碰私有字段，游戏改版也不易失效。
    //
    // 为什么必须持续写：CastSoulFlame() 每次施放都会把 m_cooldownRemaining 置 1，
    // 之后 HandleCooldown() 每帧递减 deltaTime / CooldownDuration，所以清一次会立刻被还原。
    public static class SoulFlameNoCooldown
    {
        private const int IntervalMs = 10;

        private static Timer _timer;

        public static void Start()
        {
            if (_timer != null) return; // 幂等：重复 Start 不重复起定时器

            _timer = new Timer(Clear, null, 0, IntervalMs);
        }

        public static void Stop()
        {
            if (_timer == null) return;

            _timer.Dispose();
            _timer = null;
        }

        private static void Clear(object state)
        {
            // 定时器回调里的未捕获异常会终止整个进程（即游戏），必须自己兜住
            try
            {
                Game.Characters.Sein.SoulFlame.FillSoulFlameBar();
            }
            catch { }
        }
    }
}
