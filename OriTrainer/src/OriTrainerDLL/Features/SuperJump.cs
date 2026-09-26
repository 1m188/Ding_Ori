using System.Threading;

namespace OriTrainerDLL.Features
{
    // 超级跳：持续把 5 个跳跃高度字段放大到原值的 Multiplier 倍。
    // 实现同终极版：站立/贴墙/移动跳各段、蹲跳、后空翻各对应一个字段，须全部放大。
    public static class SuperJump
    {
        private const float Multiplier = 2.5f;
        private const int IntervalMs = 10;

        private static Timer _timer;

        private static SeinJump _jump; // 上次捕获原值时的实例，用于识别组件重建
        private static float _first, _second, _third, _crouch, _backflip;

        public static void Start()
        {
            if (_timer != null) return; // 幂等：重复 Start 不重复起定时器

            _timer = new Timer(Apply, null, 0, IntervalMs);
        }

        public static void Stop()
        {
            if (_timer == null) return;

            _timer.Dispose();
            _timer = null;

            // 定时器回调里的未捕获异常会终止整个进程（即游戏），必须自己兜住
            try { Restore(); }
            catch { }
        }

        private static void Apply(object state)
        {
            try
            {
                SeinJump jump = Current();
                if (jump == null) return;

                // 首次拿到 / 组件被重建：按当前实例捕获原值。
                // 用 Unity 的 != 而不是 ReferenceEquals：前者能正确识别"旧实例已销毁"。
                if (jump != _jump)
                {
                    _jump = jump;
                    _first = jump.FirstJumpHeight;
                    _second = jump.SecondJumpHeight;
                    _third = jump.ThirdJumpHeight;
                    _crouch = jump.CrouchJumpHeight;
                    _backflip = jump.BackflipJumpHeight;
                }

                jump.FirstJumpHeight = _first * Multiplier;
                jump.SecondJumpHeight = _second * Multiplier;
                jump.ThirdJumpHeight = _third * Multiplier;
                jump.CrouchJumpHeight = _crouch * Multiplier;
                jump.BackflipJumpHeight = _backflip * Multiplier;
            }
            catch { }
        }

        private static void Restore()
        {
            // 从未成功捕获过原值就还原，会把 0 写进跳跃高度（跳不起来），必须挡住
            if (_jump == null) return;

            SeinJump jump = Current();
            if (jump == null) return;

            jump.FirstJumpHeight = _first;
            jump.SecondJumpHeight = _second;
            jump.ThirdJumpHeight = _third;
            jump.CrouchJumpHeight = _crouch;
            jump.BackflipJumpHeight = _backflip;
            _jump = null;
        }

        // 主菜单 / 读档过程中 Sein 或组件为 null，逐层判空
        private static SeinJump Current()
        {
            SeinCharacter sein = Game.Characters.Sein;
            if (sein == null) return null;

            SeinAbilities abilities = sein.Abilities;
            if (abilities == null) return null;

            return abilities.Jump;
        }
    }
}
