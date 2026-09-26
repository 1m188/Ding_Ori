using System;
using System.Reflection;
using System.Threading;

namespace OriTrainerDLL.Features
{
    // 可在不安全区域 / 不平稳地面建立灵魂链接。
    //
    // 原理：这两处并不是"施放被拦"，而是 HandleCharging() 走了回退分支，
    // 蓄力永远涨不到 1（不安全时执行 m_holdDownTime -= deltaTime / HoldDownDuration）。
    // 而施放判定本身不含任何安全检查，只要求：
    //     m_holdDownTime == 1 && IsOnGround && m_delayOnGround == 0
    //
    // 所以持续做三件事：抑制回退（HoldDownDuration = +Inf）、清落地延迟、直接写满蓄力。
    //
    // 防连发：CastSoulFlame() 内部会 PerformSave() 存档并累加施放计数，每 10ms 写满
    // 会被连续触发（疯狂刷存档）。因此以"一次按住"为单位只武装一次，观察到
    // m_holdDownTime 被清 0（= CastSoulFlame 已执行）后本次按键不再干预，松开才重置。
    public static class SoulFlameAnywhere
    {
        private const int IntervalMs = 10;

        // HoldDownDuration 实测运行时为 0.5 —— Unity 序列化覆盖了构造函数里的 0.7，
        // 所以这个字面量只用作"上一次残留了异常值"时的兜底，不作正常还原值。
        private const float FallbackHoldDownDuration = 0.5f;

        private static readonly BindingFlags Private =
            BindingFlags.NonPublic | BindingFlags.Instance;

        // 这几个字段都是 private，只能反射（游戏没有暴露可用的公开入口）
        private static FieldInfo _fCasting;       // m_isCasting
        private static FieldInfo _fHoldDown;      // m_holdDownTime
        private static FieldInfo _fDelayOnGround; // m_delayOnGround
        private static FieldInfo _fTapRemaining;  // m_tapRemainingTime

        private static Timer _timer;

        private static float _durationOrig;    // 被改成 +Inf 之前的原值
        private static bool _durationDirty;    // 当前是否处于 +Inf 状态
        private static bool _armed;            // 本次按住已写满蓄力，等游戏施放
        private static bool _castDone;         // 本次按住已施放，松开前不再干预

        public static void Start()
        {
            if (_timer != null) return; // 幂等：重复 Start 不重复起定时器

            Type t = typeof(SeinSoulFlame);
            _fCasting = t.GetField("m_isCasting", Private);
            _fHoldDown = t.GetField("m_holdDownTime", Private);
            _fDelayOnGround = t.GetField("m_delayOnGround", Private);
            _fTapRemaining = t.GetField("m_tapRemainingTime", Private);

            // 字段名对不上就直接失败（Loader 会记进错误日志），而不是每 10ms 静默空转
            if (_fCasting == null || _fHoldDown == null || _fDelayOnGround == null || _fTapRemaining == null)
                throw new Exception("SeinSoulFlame 的字段名与预期不符，功能无法工作");

            _timer = new Timer(Tick, null, 0, IntervalMs);
        }

        public static void Stop()
        {
            if (_timer == null) return;

            _timer.Dispose();
            _timer = null;

            try
            {
                SeinCharacter sein = Game.Characters.Sein;
                Restore(sein?.SoulFlame);
            }
            catch { }
            _armed = _castDone = false;
        }

        private static void Tick(object state)
        {
            // 定时器回调里的未捕获异常会终止整个进程（即游戏），必须自己兜住
            try
            {
                // 主菜单/读档过程中 Sein 为 null，显式判空以免每 10ms 抛一次异常
                SeinCharacter sein = Game.Characters.Sein;
                if (sein == null) return;

                SeinSoulFlame sf = sein.SoulFlame;
                if (sf == null) return;

                // 松开链接键：还原蓄力时长，本次按键状态清零
                if (!(bool)_fCasting.GetValue(sf))
                {
                    Restore(sf);
                    _armed = _castDone = false;
                    return;
                }

                if (_castDone) return; // 本次按住已施放，等松开再武装

                // 已写满蓄力：等 CastSoulFlame() 把 m_holdDownTime 清 0，即施放完成
                if (_armed)
                {
                    if ((float)_fHoldDown.GetValue(sf) == 0f) _castDone = true;
                    return;
                }

                // 存档点内不干预：保留"轻点开技能树"，也避免在这里被强制施放
                if (sf.InsideCheckpointMarker) return;

                // 轻点窗口内不干预
                if ((float)_fTapRemaining.GetValue(sf) > 0f) return;

                Arm(sf);
            }
            catch { }
        }

        private static void Arm(SeinSoulFlame sf)
        {
            _durationOrig = sf.HoldDownDuration;

            // 防御：若上次留下 +Inf/异常值（例如按住期间关掉了修改器），回退到默认值。
            // 否则会把这个异常值当成"原值"存下来，还原后蓄力再也满不了。
            if (float.IsInfinity(_durationOrig) || float.IsNaN(_durationOrig) || _durationOrig <= 0f)
                _durationOrig = FallbackHoldDownDuration;

            sf.HoldDownDuration = float.PositiveInfinity; // 回退量 delta/Inf = 0，蓄力不再被扣
            _durationDirty = true;

            _fDelayOnGround.SetValue(sf, 0f); // 施放判定要求落地延迟归零
            _fHoldDown.SetValue(sf, 1f);      // 不安全时累加分支不会执行，只能直接写满
            _armed = true;
        }

        private static void Restore(SeinSoulFlame sf)
        {
            if (!_durationDirty) return;

            if (sf != null) sf.HoldDownDuration = _durationOrig;
            _durationDirty = false;
        }
    }
}
