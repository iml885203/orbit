import os
import sys


NATIVE_MKTEMP = None


def routed_arguments(arguments):
    explicit = False
    index = 0
    while index < len(arguments):
        argument = arguments[index]
        if argument == "--":
            explicit = index + 1 < len(arguments)
            break
        if not argument.startswith("-") or argument == "-":
            explicit = True
            break
        options = argument[1:]
        while options:
            option, options = options[0], options[1:]
            if option == "p":
                return arguments
            if option == "t":
                if not options:
                    index += 1
                    if index == len(arguments):
                        return arguments
                break
            if option not in "dqu":
                return arguments
        index += 1
    if explicit:
        return arguments
    return ["-p", os.environ["TMPDIR"], *arguments]


if __name__ == "__main__":
    os.execv(NATIVE_MKTEMP, [NATIVE_MKTEMP, *routed_arguments(sys.argv[1:])])
