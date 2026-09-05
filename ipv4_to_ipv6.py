#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
IPv4到IPv6地址转换脚本
将IPv4地址转换为fd88::ffff:前缀的IPv6地址格式

完全独立版本，无循环导入问题，包含完整的转换函数
"""

import sys
import ipaddress
import argparse


def ipv4_to_ipv6(ipv4_str, prefix_length=64):
    """
    将IPv4地址转换为标准IPv6地址

    Args:
        ipv4_str (str): IPv4地址字符串
        prefix_length (int): IPv6前缀长度，默认为64

    Returns:
        str: 转换后的标准IPv6地址

    Raises:
        ValueError: 当IPv4地址格式无效时
    """
    try:
        # 验证IPv4地址格式
        ipv4_obj = ipaddress.IPv4Address(ipv4_str)

        # 将IPv4地址转换为十六进制
        ipv4_hex = ipv4_obj.packed.hex()

        # 构建标准IPv6地址：fd88::ffff: + IPv4十六进制
        # 格式：fd88:0000:0000:0000:0000:ffff:xxxx:xxxx
        ipv6_str = f"fd88:0000:0000:0000:0000:ffff:{ipv4_hex[:4]}:{ipv4_hex[4:]}"

        # 压缩IPv6地址（移除前导零）
        ipv6_str = ipaddress.IPv6Address(ipv6_str).compressed

        # 添加前缀长度
        if prefix_length:
            ipv6_str += f"/{prefix_length}"

        return ipv6_str

    except ipaddress.AddressValueError as e:
        raise ValueError(f"无效的IPv4地址: {ipv4_str}") from e


def validate_prefix_length(prefix_length):
    """
    验证IPv6前缀长度是否有效

    Args:
        prefix_length (int): 前缀长度

    Returns:
        bool: 是否有效

    Raises:
        ValueError: 当前缀长度无效时
    """
    if not isinstance(prefix_length, int):
        raise ValueError("前缀长度必须是整数")

    if prefix_length < 0 or prefix_length > 128:
        raise ValueError("IPv6前缀长度必须在0-128之间")

    return True


def convert_ipv4_to_ipv6(ipv4_str, prefix_length=64):
    """
    完整的IPv4到IPv6转换函数（包含验证）

    Args:
        ipv4_str (str): IPv4地址字符串
        prefix_length (int): IPv6前缀长度，默认为64

    Returns:
        str: 转换后的IPv6地址

    Raises:
        ValueError: 当输入参数无效时
    """
    # 验证前缀长度
    validate_prefix_length(prefix_length)

    # 执行转换
    return ipv4_to_ipv6(ipv4_str, prefix_length)


def main():
    """主函数 - 命令行接口"""
    parser = argparse.ArgumentParser(
        description="将IPv4地址转换为IPv6地址",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
示例:
  python ipv4_to_ipv6.py 10.88.0.1
  python ipv4_to_ipv6.py 192.168.1.1 --prefix 48
  python ipv4_to_ipv6.py 172.16.0.1 -p 128
  python ipv4_to_ipv6.py 8.8.8.8 --prefix 96
        """
    )

    parser.add_argument(
        "ipv4_address",
        help="要转换的IPv4地址"
    )

    parser.add_argument(
        "-p", "--prefix",
        type=int,
        default=64,
        help="IPv6前缀长度 (默认: 64, 范围: 0-128)"
    )

    args = parser.parse_args()

    try:
        # 使用完整的转换函数
        ipv6_result = convert_ipv4_to_ipv6(args.ipv4_address, args.prefix)
        print(ipv6_result)

    except ValueError as e:
        print(f"错误: {e}", file=sys.stderr)
        sys.exit(1)
    except Exception as e:
        print(f"未知错误: {e}", file=sys.stderr)
        sys.exit(1)


# 如果作为模块导入，提供便捷的转换函数
if __name__ != "__main__":
    # 当作为模块导入时，提供简化的接口
    def convert(ipv4_str, prefix_length=64):
        """
        便捷的转换函数，用于模块导入

        Args:
            ipv4_str (str): IPv4地址
            prefix_length (int): 前缀长度，默认64

        Returns:
            str: IPv6地址
        """
        return convert_ipv4_to_ipv6(ipv4_str, prefix_length)


if __name__ == "__main__":
    main()
